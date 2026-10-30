package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func signedReport(t *testing.T, s *server) cacheHitReport {
	t.Helper()
	receipt, err := s.issueReceipt("/d/"+testID, testHref, 1793347985000)
	if err != nil {
		t.Fatal(err)
	}
	return cacheHitReport{PublicPath: "/d/" + testID, Href: testHref, ExpiresAt: json.Number("1793347985000"), Receipt: receipt}
}
func errorCode(err error) string {
	var api *apiError
	if errors.As(err, &api) {
		return api.code
	}
	return ""
}
func TestSignedReportsAndReplay(t *testing.T) {
	s, host := fixture(t, nil)
	good := signedReport(t, s)
	variants := []cacheHitReport{good, good, good, good}
	variants[0].Receipt = ""
	variants[1].Href += "&different=1"
	variants[2].PublicPath = "/i/" + testID
	variants[3].ExpiresAt = json.Number("1793347984999")
	for _, body := range variants {
		res, raw := request(t, host, "POST", "/api/stats/cache-hit", body)
		requireCode(t, res, raw, 400, "INVALID_RECEIPT")
	}
	res, raw := request(t, host, "POST", "/api/stats/cache-hit", good)
	requireCode(t, res, raw, 200, "")
	var reply statistics
	_ = json.Unmarshal(raw, &reply)
	if reply.Receipt == "" || reply.Receipt == good.Receipt || reply.ConvertedLinks != 1 {
		t.Fatal("receipt was not rotated")
	}
	res, raw = request(t, host, "POST", "/api/stats/cache-hit", good)
	requireCode(t, res, raw, 409, "RECEIPT_REPLAYED")
	good.Receipt = reply.Receipt
	res, raw = request(t, host, "POST", "/api/stats/cache-hit", good)
	requireCode(t, res, raw, 200, "")
	if counterValue(t, s) != 2 {
		t.Fatal("forged/repeated receipts were counted")
	}
}
func TestConcurrentReceiptConsumption(t *testing.T) {
	s, _ := fixture(t, nil)
	body := signedReport(t, s)
	var group sync.WaitGroup
	var accepted atomic.Int64
	for range 30 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := s.recordCacheHit(context.Background(), body, "127.0.0.1")
			if err == nil {
				accepted.Add(1)
			} else if errorCode(err) != "RECEIPT_REPLAYED" {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 1 || counterValue(t, s) != 1 {
		t.Fatal("a receipt was consumed more than once")
	}
}
func TestReceiptsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter.sqlite")
	first, err := openCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(first)
	s.now = func() time.Time { return testNow }
	body := signedReport(t, s)
	s.stopStreams()
	first.close()
	second, err := openCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	s = newServer(second)
	s.now = func() time.Time { return testNow }
	if _, err = s.recordCacheHit(context.Background(), body, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	s.stopStreams()
	second.close()
	third, err := openCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer third.close()
	s = newServer(third)
	defer s.stopStreams()
	s.now = func() time.Time { return testNow }
	if _, err = s.recordCacheHit(context.Background(), body, "127.0.0.1"); errorCode(err) != "RECEIPT_REPLAYED" {
		t.Fatalf("replay after restart: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("counter secret file is not private")
	}
}
func TestReceiptAgeLimit(t *testing.T) {
	s, _ := fixture(t, nil)
	deadline := testNow.Add(72 * time.Hour)
	href := fmt.Sprintf("https://downloader.disk.yandex.ru/disk/sig/%x/token", deadline.Unix())
	receipt, err := s.issueReceipt("/d/"+testID, href, deadline.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	body := cacheHitReport{PublicPath: "/d/" + testID, Href: href, ExpiresAt: json.Number(fmt.Sprint(deadline.UnixMilli())), Receipt: receipt}
	s.now = func() time.Time { return testNow.Add(25 * time.Hour) }
	if _, err = s.recordCacheHit(context.Background(), body, "127.0.0.1"); errorCode(err) != "EXPIRED_RECEIPT" {
		t.Fatal(err)
	}
}
func TestCounterBudgetDoesNotBreakDownloads(t *testing.T) {
	s, host := fixture(t, func(w http.ResponseWriter, r *http.Request) { jsonBody(w, map[string]string{"href": testHref}) })
	for i := 0; i < 12; i++ {
		res, body := request(t, host, "GET", "/api/download/d/"+testID, nil)
		requireCode(t, res, body, 200, "")
	}
	if counterValue(t, s) != 5 {
		t.Fatal("counter burst not bounded")
	}
	res, body := request(t, host, "GET", "/api/download/d/"+testID, nil)
	requireCode(t, res, body, 429, "RATE_LIMITED")
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("missing retry deadline")
	}
}
func TestRateLimitsIgnoreSpoofedHeaders(t *testing.T) {
	s := newServer(nil)
	defer s.stopStreams()
	for i := 0; i < 13; i++ {
		r := httptest.NewRequest("GET", "http://localhost/api/download/d/"+testID, nil)
		r.RemoteAddr = "192.0.2.1:1000"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		if s.clientIP(r) != "192.0.2.1" {
			t.Fatal("trusted a forged header")
		}
		if allowed := s.guard.allow(s.clientIP(r), "api", testNow); allowed != (i < 12) {
			t.Fatal("rate limit bypass")
		}
	}
	var err error
	s.trustedProxies, err = trustedProxyRanges("127.0.0.1,10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.99, 192.0.2.22, 10.1.2.3")
	if s.clientIP(r) != "192.0.2.22" {
		t.Fatal("trusted the leftmost forged proxy header")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.99, invalid")
	if s.clientIP(r) != "127.0.0.1" {
		t.Fatal("accepted malformed proxy chain")
	}
	if _, err = trustedProxyRanges("not-an-address"); err == nil {
		t.Fatal("accepted invalid proxy configuration")
	}
	r.RemoteAddr = "[2001:db8::1]:1234"
	first := s.clientIP(r)
	r.RemoteAddr = "[2001:db8::222]:5678"
	if s.clientIP(r) != first {
		t.Fatal("IPv6 address rotation bypasses the subnet limit")
	}
}
func TestLimitsAreBoundedAndRefill(t *testing.T) {
	g := newAbuseGuard()
	for i := 0; i < maxClients; i++ {
		g.allow(fmt.Sprint(i), "api", testNow)
	}
	if g.allow("new-client", "api", testNow) || len(g.clients) > maxClients {
		t.Fatal("unbounded limiter memory")
	}
	if !g.allow("new-client", "api", testNow.Add(11*time.Minute)) {
		t.Fatal("stale limiter entries were not released")
	}
	g = newAbuseGuard()
	for range 5 {
		if !g.allow("ip", "count", testNow) {
			t.Fatal("initial budget")
		}
	}
	if g.allow("ip", "count", testNow) {
		t.Fatal("unlimited counter increments")
	}
	if !g.allow("ip", "count", testNow.Add(10*time.Second)) {
		t.Fatal("counter budget did not refill")
	}
}
func TestWebSocketLimitsAndReplay(t *testing.T) {
	s, host := fixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var conns []*websocket.Conn
	defer func() {
		for _, conn := range conns {
			conn.CloseNow()
		}
	}()
	for range 4 {
		conn, _, err := websocket.Dial(ctx, "ws"+host.URL[4:]+"/api/stats/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		socketRead(t, conn, func(m statsMessage) bool { return m.Type == "stats" })
	}
	conn, response, err := websocket.Dial(ctx, "ws"+host.URL[4:]+"/api/stats/ws", nil)
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil || response.StatusCode != 429 {
		t.Fatal("unlimited WebSocket connections")
	}
	body := signedReport(t, s)
	command := statsCommand{Type: "cache-hit", ID: 1, cacheHitReport: body}
	if err := wsjson.Write(ctx, conns[0], command); err != nil {
		t.Fatal(err)
	}
	socketRead(t, conns[0], func(m statsMessage) bool { return m.ID == 1 && m.Receipt != "" })
	res, raw := request(t, host, "POST", "/api/stats/cache-hit", body)
	requireCode(t, res, raw, 409, "RECEIPT_REPLAYED")
	if counterValue(t, s) != 1 {
		t.Fatal("cross-transport replay counted")
	}
}
func TestCrossOriginAndURIProtection(t *testing.T) {
	s := newServer(nil)
	defer s.stopStreams()
	for _, headers := range []http.Header{{"Origin": {"https://evil.test"}}, {"Sec-Fetch-Site": {"cross-site"}}, {"Origin": {"null"}}} {
		r := httptest.NewRequest("POST", "http://localhost/api/stats/cache-hit", bytes.NewBufferString("{}"))
		r.Header = headers
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("cross-origin request accepted")
		}
	}
	r := httptest.NewRequest("GET", "http://localhost/"+string(bytes.Repeat([]byte("x"), 5000)), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 414 {
		t.Fatal("unbounded request URL")
	}
}
func TestUpstreamRedirectAndHostValidation(t *testing.T) {
	var reached atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer upstream.Close()
	s := newServer(nil)
	defer s.stopStreams()
	s.apiBase = upstream.URL
	if _, err := s.resolveLink(context.Background(), "d", testID, "", false); err == nil {
		t.Fatal("upstream redirect accepted")
	}
	if reached.Load() != 0 {
		t.Fatal("server followed an unexpected upstream redirect")
	}
	for _, href := range []string{"https://evil.test/file", "https://127.0.0.1/file", "https://downloader.disk.yandex.ru:443/file", "https://downloader.disk.yandex.ru.evil.test/file"} {
		if validDownload(href) {
			t.Fatalf("untrusted redirect: %s", href)
		}
	}
}
func TestCounterDoesNotOverflowJSInteger(t *testing.T) {
	s, _ := fixture(t, nil)
	if _, err := s.counter.db.Exec("UPDATE conversion_counter SET converted_links=9007199254740991"); err != nil {
		t.Fatal(err)
	}
	stats, err := s.counter.increment(context.Background())
	if err != nil || stats.ConvertedLinks != 9007199254740991 {
		t.Fatal("counter overflow", err)
	}
}

func TestWebSocketFloodReleasesCapacity(t *testing.T) {
	s, host := fixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+host.URL[4:]+"/api/stats/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	socketRead(t, conn, func(m statsMessage) bool { return m.Type == "stats" })
	for i := 0; i < 30; i++ {
		if wsjson.Write(ctx, conn, map[string]any{"type": "get", "id": i}) != nil {
			break
		}
	}
	for {
		var message statsMessage
		if wsjson.Read(ctx, conn, &message) != nil {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("command flood did not close the socket promptly")
	}
	for i := 0; i < 100; i++ {
		s.guard.mu.Lock()
		streams := s.guard.streams
		s.guard.mu.Unlock()
		if streams == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("closed WebSocket still consumes connection capacity")
}
func TestGlobalWebSocketAndUpstreamCapacity(t *testing.T) {
	g := newAbuseGuard()
	var releases []func()
	for i := 0; i < 128; i++ {
		release, ok := g.stream(fmt.Sprint(i), testNow)
		if !ok {
			t.Fatal("capacity too small")
		}
		releases = append(releases, release)
	}
	if _, ok := g.stream("excess", testNow); ok {
		t.Fatal("unbounded stream capacity")
	}
	for _, release := range releases {
		release()
		release()
	}
	if _, ok := g.stream("excess", testNow); !ok {
		t.Fatal("capacity not restored")
	}
	s := newServer(nil)
	defer s.stopStreams()
	for i := 0; i < cap(s.upstreamSlots); i++ {
		s.upstreamSlots <- struct{}{}
	}
	if _, err := s.resolveLink(context.Background(), "d", testID, "", false); errorCode(err) != "SERVER_BUSY" {
		t.Fatal("upstream concurrency limit failed", err)
	}
}

func TestReceiptEncodingCannotBypassReplayProtection(t *testing.T) {
	s, _ := fixture(t, nil)
	report := signedReport(t, s)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	original := report.Receipt
	last := strings.IndexByte(alphabet, original[len(original)-1])
	// A permissive base64 decoder accepts non-zero pad bits as the same signature.
	report.Receipt = original[:len(original)-1] + string(alphabet[last^1])
	if _, err := s.recordCacheHit(context.Background(), report, "127.0.0.1"); errorCode(err) != "INVALID_RECEIPT" {
		t.Fatal("non-canonical receipt accepted", err)
	}
	report.Receipt = original[:10] + "\n" + original[10:]
	if _, err := s.recordCacheHit(context.Background(), report, "127.0.0.1"); errorCode(err) != "INVALID_RECEIPT" {
		t.Fatal("receipt alias accepted", err)
	}
	report.Receipt = original
	if _, err := s.recordCacheHit(context.Background(), report, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if counterValue(t, s) != 1 {
		t.Fatal("invalid receipt encoding counted")
	}
}

func TestNextcloudStyleRealIPDefaults(t *testing.T) {
	s := newServer(nil)
	defer s.stopStreams()
	for _, peer := range []string{"10.0.0.1:1234", "10.255.255.254:1234", "172.16.0.1:1234", "172.18.0.2:1234", "172.31.255.254:1234", "192.168.1.2:1234", "[::ffff:172.18.0.2]:1234"} {
		r := httptest.NewRequest("GET", "http://localhost/", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Real-IP", "203.0.113.15")
		r.Header.Set("X-Forwarded-For", "198.51.100.99")
		if actual := s.clientIP(r); actual != "203.0.113.15" {
			t.Fatalf("proxy %s resolved to %s", peer, actual)
		}
	}
	for _, peer := range []string{"172.15.255.254:1234", "172.32.0.1:1234", "192.169.0.1:1234", "203.0.113.1:1234", "127.0.0.1:1234", "[fd00::1]:1234"} {
		r := httptest.NewRequest("GET", "http://localhost/", nil)
		r.RemoteAddr = peer
		expected := s.clientIP(r)
		r.Header.Set("X-Real-IP", "203.0.113.15")
		if s.clientIP(r) != expected {
			t.Fatalf("trusted an address outside the default ranges: %s", peer)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.RemoteAddr = "172.18.0.2:1234"
	for _, header := range []string{"not-an-ip", "203.0.113.1, 203.0.113.2", "203.0.113.1:1234", "127.0.0.1", "::1", "::", "224.0.0.1", "fe80::1%eth0"} {
		r.Header.Set("X-Real-IP", header)
		if s.clientIP(r) != "172.18.0.2" {
			t.Fatalf("accepted malformed or non-unicast X-Real-IP: %s", header)
		}
	}
	r.Header["X-Real-Ip"] = []string{"203.0.113.1", "203.0.113.2"}
	if s.clientIP(r) != "172.18.0.2" {
		t.Fatal("accepted duplicate X-Real-IP headers")
	}
	r.Header.Set("X-Real-IP", "2001:db8::1234")
	if s.clientIP(r) != "2001:db8::/64" {
		t.Fatal("IPv6 client subnet limits not applied")
	}
}
func TestExplicitProxiesReplaceAutomaticRealIPRewrite(t *testing.T) {
	s := newServer(nil)
	defer s.stopStreams()
	var err error
	s.trustedProxies, err = trustedProxyRanges("192.168.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.RemoteAddr = "172.18.0.2:1234"
	r.Header.Set("X-Real-IP", "203.0.113.15")
	r.Header.Set("X-Forwarded-For", "198.51.100.25")
	if s.clientIP(r) != "172.18.0.2" {
		t.Fatal("automatic rewrite was not disabled")
	}
	s.trustedProxies, err = trustedProxyRanges("172.18.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if s.clientIP(r) != "198.51.100.25" {
		t.Fatal("explicit trusted proxy did not use X-Forwarded-For")
	}
}
