package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const receiptLifetime = 24 * time.Hour
const maxClients = 4096

var defaultRewriteProxies = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func isDefaultRewriteProxy(ip netip.Addr) bool {
	for _, prefix := range defaultRewriteProxies {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
func clientIPKey(ip netip.Addr) string {
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

type bucket struct {
	tokens  float64
	updated time.Time
}

func (b *bucket) allow(now time.Time, rate, burst float64) bool {
	if b.updated.IsZero() {
		b.tokens = burst
	} else if now.After(b.updated) {
		b.tokens = min(burst, b.tokens+now.Sub(b.updated).Seconds()*rate)
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type clientLimits struct {
	api, commands, counts bucket
	lastSeen              time.Time
	streams               int
}
type abuseGuard struct {
	mu                    sync.Mutex
	clients               map[string]*clientLimits
	api, commands, counts bucket
	streams               int
	cleaned               time.Time
}

func newAbuseGuard() *abuseGuard { return &abuseGuard{clients: make(map[string]*clientLimits)} }
func (g *abuseGuard) client(ip string, now time.Time) *clientLimits {
	if g.cleaned.IsZero() || now.Sub(g.cleaned) >= time.Minute {
		for key, c := range g.clients {
			if c.streams == 0 && now.Sub(c.lastSeen) > 10*time.Minute {
				delete(g.clients, key)
			}
		}
		g.cleaned = now
	}
	c := g.clients[ip]
	if c == nil {
		if len(g.clients) >= maxClients {
			return nil
		}
		c = &clientLimits{}
		g.clients[ip] = c
	}
	c.lastSeen = now
	return c
}
func (g *abuseGuard) allow(ip, kind string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.client(ip, now)
	if c == nil {
		return false
	}
	switch kind {
	case "count":
		return c.counts.allow(now, .1, 5) && g.counts.allow(now, 10, 30)
	case "command":
		return c.commands.allow(now, 5, 20) && g.commands.allow(now, 100, 200)
	default:
		return c.api.allow(now, .5, 12) && g.api.allow(now, 30, 60)
	}
}
func (g *abuseGuard) stream(ip string, now time.Time) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.client(ip, now)
	if c == nil || c.streams >= 4 || g.streams >= 128 {
		return nil, false
	}
	c.streams++
	g.streams++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); c.streams--; g.streams--; g.mu.Unlock() }) }, true
}
func trustedProxyRanges(value string) ([]netip.Prefix, error) {
	var ranges []netip.Prefix
	if strings.TrimSpace(value) == "" {
		return ranges, nil
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			if address, e := netip.ParseAddr(part); e == nil {
				prefix = netip.PrefixFrom(address, address.BitLen())
			} else {
				return nil, fmt.Errorf("invalid TRUSTED_PROXIES entry: %q", part)
			}
		}
		ranges = append(ranges, prefix.Masked())
	}
	return ranges, nil
}
func (s *server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	ip = ip.Unmap()
	// Match the Nextcloud Apache image: trust X-Real-IP from RFC1918 peers by default.
	// An explicit trusted proxy list replaces these defaults and uses X-Forwarded-For.
	if len(s.trustedProxies) == 0 && isDefaultRewriteProxy(ip) {
		values := r.Header.Values("X-Real-IP")
		if len(values) == 1 {
			real, err := netip.ParseAddr(strings.TrimSpace(values[0]))
			if err == nil && real.Zone() == "" && real.IsGlobalUnicast() {
				return clientIPKey(real.Unmap())
			}
		}
	}
	trusted := func(address netip.Addr) bool {
		for _, prefix := range s.trustedProxies {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	if trusted(ip) {
		// Walk from the trusted peer toward the client, never trust an arbitrary leftmost header.
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0 && trusted(ip); i-- {
			next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if err != nil {
				break
			}
			ip = next.Unmap()
		}
	}
	return clientIPKey(ip)
}
func sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, r.Host) {
			return false
		}
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site" || (r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document" && r.Header.Get("Sec-Fetch-User") == "?1")
}
func receiptMAC(key []byte, path, href string, expires int64, nonce []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(nonce)
	mac.Write([]byte(path))
	mac.Write([]byte{0})
	mac.Write([]byte(href))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return mac.Sum(nil)
}
func (s *server) issueReceipt(path, href string, expires int64) (string, error) {
	if s.counter == nil {
		return "", nil
	}
	nonce := make([]byte, 24)
	binary.BigEndian.PutUint64(nonce, uint64(s.now().UnixMilli()))
	if _, err := rand.Read(nonce[8:]); err != nil {
		return "", err
	}
	mac := receiptMAC(s.counter.key, path, href, expires, nonce)
	return base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}
func (s *server) verifyReceipt(body cacheHitReport) ([32]byte, time.Time, error) {
	invalid := failure(400, "INVALID_RECEIPT", "A server-issued download receipt is required.")
	parts := strings.Split(body.Receipt, ".")
	if len(body.Receipt) != 76 || len(parts) != 2 {
		return [32]byte{}, time.Time{}, invalid
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(nonce) != 24 {
		return [32]byte{}, time.Time{}, invalid
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(signature) != 32 {
		return [32]byte{}, time.Time{}, invalid
	}
	expires, err := body.ExpiresAt.Int64()
	if err != nil {
		return [32]byte{}, time.Time{}, invalid
	}
	if !hmac.Equal(signature, receiptMAC(s.counter.key, body.PublicPath, body.Href, expires, nonce)) {
		return [32]byte{}, time.Time{}, invalid
	}
	issued := time.UnixMilli(int64(binary.BigEndian.Uint64(nonce)))
	deadline := issued.Add(receiptLifetime)
	if issued.After(s.now().Add(time.Minute)) || !deadline.After(s.now()) {
		return [32]byte{}, time.Time{}, failure(400, "EXPIRED_RECEIPT", "The cached download receipt has expired.")
	}
	return sha256.Sum256(signature), deadline, nil
}
func (s *server) countConversion(ctx context.Context, ip string) (statistics, error) {
	if s.counter != nil && s.guard.allow(ip, "count", s.now()) {
		return s.counter.increment(ctx)
	}
	return s.counter.current(ctx)
}
