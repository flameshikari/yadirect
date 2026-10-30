package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type server struct {
	noCache        bool
	guard          *abuseGuard
	trustedProxies []netip.Prefix
	upstreamSlots  chan struct{}
	counter        *counter
	cache          *linkCache
	client         *http.Client
	apiBase        string
	timeout        time.Duration
	now            func() time.Time
	page           []byte
	compressed     []byte
	csp            string
	etag           string
	streams        context.Context
	stopStreams    context.CancelFunc
}

var routePattern = regexp.MustCompile(`^/(?:(list|api/download)/)?([di])/([\w-]+)(?:/(.*))?$`)

func newServer(counter *counter) *server {
	streams, cancel := context.WithCancel(context.Background())
	s := &server{guard: newAbuseGuard(), upstreamSlots: make(chan struct{}, 32), counter: counter, cache: newLinkCache(), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, apiBase: "https://cloud-api.yandex.net", timeout: 10 * time.Second, now: time.Now, page: embeddedPage, streams: streams, stopStreams: cancel}
	page := embeddedPage
	if counter != nil {
		page = bytes.Replace(page, []byte(`name="yadirect-counter" content="disabled"`), []byte(`name="yadirect-counter" content="enabled"`), 1)
	}
	s.preparePage(page)
	return s
}
func (s *server) preparePage(page []byte) {
	s.page = page
	hashes := func(tag string) string {
		pattern := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>(.*?)</` + tag + `>`)
		var values []string
		for _, match := range pattern.FindAllSubmatch(s.page, -1) {
			sum := sha256.Sum256(match[1])
			values = append(values, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
		return strings.Join(values, " ")
	}
	s.csp = "default-src 'self';base-uri 'self';object-src 'none';frame-ancestors 'self';form-action 'self';script-src " + hashes("script") + ";script-src-attr 'none';style-src " + hashes("style") + ";connect-src 'self';font-src data:;img-src data:"
	sum := sha256.Sum256(s.page)
	s.etag = fmt.Sprintf(`"%x"`, sum)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(s.page)
	_ = writer.Close()
	s.compressed = compressed.Bytes()
}
func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/stats/ws" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
	}
	headers := w.Header()
	headers.Set("Content-Security-Policy", s.csp)
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "SAMEORIGIN")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("Cross-Origin-Opener-Policy", "same-origin")
	headers.Set("Cross-Origin-Resource-Policy", "same-origin")
	headers.Set("Access-Control-Expose-Headers", "X-Converted-Links")
	headers.Set("Cache-Control", "no-store")
	if len(r.URL.RequestURI()) > 4096 {
		writeJSON(w, r, 414, map[string]string{"error": "URI_TOO_LONG"})
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "/health" {
		if !sameOrigin(r) {
			writeJSON(w, r, 403, map[string]string{"error": "CROSS_ORIGIN_REQUEST"})
			return
		}
		kind := "api"
		if strings.HasPrefix(r.URL.Path, "/api/stats") {
			kind = "command"
		}
		if !s.guard.allow(s.clientIP(r), kind, s.now()) {
			headers.Set("Retry-After", "2")
			writeJSON(w, r, 429, map[string]string{"error": "RATE_LIMITED"})
			return
		}
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	err := s.dispatch(w, r)
	if err != nil {
		var api *apiError
		if !errors.As(err, &api) {
			log.Printf("request failed: %v", err)
			api = &apiError{500, "INTERNAL_ERROR", "An unexpected error occurred. Please try again."}
		}
		writeJSON(w, r, api.status, map[string]string{"error": api.code, "message": api.message})
	}
}
func (s *server) dispatch(w http.ResponseWriter, r *http.Request) error {
	if s.counter == nil && (r.URL.Path == "/api/stats" || strings.HasPrefix(r.URL.Path, "/api/stats/")) {
		return failure(404, "COUNTER_DISABLED", "The hit counter is disabled.")
	}
	isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch r.URL.Path {
	case "/health":
		if !isRead {
			return failure(405, "METHOD_NOT_ALLOWED", "Method not allowed.")
		}
		writeJSON(w, r, 200, map[string]string{"status": "ok"})
		return nil
	case "/", "/index.html":
		if !isRead {
			return failure(405, "METHOD_NOT_ALLOWED", "Method not allowed.")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", s.etag)
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Header.Get("If-None-Match") == s.etag {
			w.WriteHeader(304)
			return nil
		}
		page := s.page
		for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
			parts := strings.Split(strings.TrimSpace(encoding), ";")
			if parts[0] != "gzip" {
				continue
			}
			allowed := true
			for _, part := range parts[1:] {
				if strings.HasPrefix(strings.TrimSpace(part), "q=") {
					quality, _ := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(part), "q="), 64)
					allowed = quality > 0
				}
			}
			if allowed {
				page = s.compressed
				w.Header().Set("Content-Encoding", "gzip")
			}
			break
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(page)
		}
		return nil
	case "/api/stats":
		if !isRead {
			return failure(405, "METHOD_NOT_ALLOWED", "Method not allowed.")
		}
		stats, err := s.counter.current(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, r, 200, stats)
		return nil
	case "/api/stats/ws":
		if r.Method != http.MethodGet {
			return failure(405, "METHOD_NOT_ALLOWED", "Method not allowed.")
		}
		return s.websocket(w, r)
	case "/api/stats/cache-hit":
		if r.Method != http.MethodPost {
			return failure(405, "METHOD_NOT_ALLOWED", "Method not allowed.")
		}
		return s.cacheHit(w, r)
	}
	if !isRead {
		return failure(404, "NOT_FOUND", "This address was not found.")
	}
	match := routePattern.FindStringSubmatch(r.URL.EscapedPath())
	if match == nil {
		return failure(404, "NOT_FOUND", "This address was not found. Open / to enter a Yandex.Disk link.")
	}
	operation, kind, key := match[1], match[2], match[3]
	if !publicKeyPattern.MatchString(key) {
		return failure(400, "INVALID_PUBLIC_KEY", "The public link ID must contain exactly 14 letters, digits, underscores or hyphens.")
	}
	folder := ""
	if match[4] != "" {
		decoded, err := url.PathUnescape(match[4])
		if err != nil {
			return failure(400, "INVALID_PATH", "The URL contains invalid encoding.")
		}
		folder = "/" + decoded
	}
	entry, err := s.resolveLink(r.Context(), kind, key, folder, operation == "list")
	if err != nil {
		return err
	}
	if operation == "list" {
		writeJSON(w, r, 200, entry.items)
		return nil
	}
	var stats statistics
	if r.Method == http.MethodGet {
		stats, err = s.countConversion(r.Context(), s.clientIP(r))
	} else {
		stats, err = s.counter.current(r.Context())
	}
	if err != nil {
		return err
	}
	if s.counter != nil {
		w.Header().Set("X-Converted-Links", strconv.FormatInt(stats.ConvertedLinks, 10))
	}
	if operation == "api/download" {
		publicPath := "/" + kind + "/" + key
		if folder != "" {
			publicPath += "/" + match[4]
		}
		receipt, err := s.issueReceipt(publicPath, entry.href, entry.expires.UnixMilli())
		if err != nil {
			return err
		}
		writeJSON(w, r, 200, map[string]any{"href": entry.href, "expiresAt": entry.expires.UnixMilli(), "receipt": receipt})
	} else {
		http.Redirect(w, r, entry.href, http.StatusFound)
	}
	return nil
}
func (s *server) cacheHit(w http.ResponseWriter, r *http.Request) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return failure(400, "INVALID_CACHE_HIT", "JSON is required.")
	}
	var body cacheHitReport
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return failure(413, "PAYLOAD_TOO_LARGE", "The request is too large.")
		}
		return failure(400, "INVALID_REQUEST", "The request contains invalid JSON.")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return failure(400, "INVALID_REQUEST", "A single JSON object is required.")
	}
	stats, err := s.recordCacheHit(r.Context(), body, s.clientIP(r))
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, stats)
	return nil
}

type cacheHitReport struct {
	PublicPath string      `json:"publicPath"`
	Href       string      `json:"href"`
	ExpiresAt  json.Number `json:"expiresAt"`
	Receipt    string      `json:"receipt"`
}

func (s *server) recordCacheHit(ctx context.Context, body cacheHitReport, ip string) (statistics, error) {
	match := routePattern.FindStringSubmatch(body.PublicPath)
	download, err := url.Parse(body.Href)
	if match == nil || match[1] != "" || !publicKeyPattern.MatchString(match[3]) || strings.ContainsAny(body.PublicPath, "?#") || err != nil || !validDownload(body.Href) || download.Port() != "" || !downloadHostPattern.MatchString(download.Hostname()) {
		return statistics{}, failure(400, "INVALID_CACHE_HIT", "A valid public path and cached Yandex.Disk download URL are required.")
	}
	expires, err := body.ExpiresAt.Int64()
	deadline, ok := downloadExpiry(body.Href)
	if !ok {
		deadline = s.now().Add(fallbackTTL)
	}
	if err != nil || expires <= s.now().UnixMilli() || expires > deadline.UnixMilli() {
		return statistics{}, failure(400, "EXPIRED_CACHE_HIT", "The cached download link has expired or has an invalid deadline.")
	}
	token, validUntil, err := s.verifyReceipt(body)
	if err != nil {
		return statistics{}, err
	}
	next, err := s.issueReceipt(body.PublicPath, body.Href, expires)
	if err != nil {
		return statistics{}, err
	}
	stats, err := s.counter.consume(ctx, token, minTime(validUntil, time.UnixMilli(expires)), s.now(), s.guard.allow(ip, "count", s.now()))
	if err != nil {
		return statistics{}, err
	}
	stats.Receipt = next
	return stats, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
