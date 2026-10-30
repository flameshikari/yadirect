package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const testID = "-goNh3dqsHqxcA"
const testHref = "https://downloader.disk.yandex.ru/disk/signature/6ae45191/token?filename=file.txt"

var testNow = time.Unix(1793330000, 0)

func jsonBody(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func fixture(t *testing.T, upstream http.HandlerFunc) (*server, *httptest.Server) {
	t.Helper()
	counter, err := openCounter(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	app := newServer(counter)
	app.now = func() time.Time { return testNow }
	if upstream != nil {
		api := httptest.NewServer(upstream)
		app.apiBase = api.URL
		app.client = api.Client()
		t.Cleanup(api.Close)
	}
	host := httptest.NewServer(app)
	t.Cleanup(func() { app.stopStreams(); host.Close(); _ = counter.close() })
	return app, host
}
func request(t *testing.T, host *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var data io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		data = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, host.URL+path, data)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *host.Client()
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, raw
}
func counterValue(t *testing.T, app *server) int64 {
	t.Helper()
	stats, err := app.counter.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return stats.ConvertedLinks
}
func requireCode(t *testing.T, response *http.Response, raw []byte, status int, code string) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("HTTP %d: %s", response.StatusCode, raw)
	}
	if code != "" {
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if body["error"] != code {
			t.Fatalf("error=%v body=%s", body["error"], raw)
		}
	}
}

func TestEmbeddedPage(t *testing.T) {
	_, host := fixture(t, nil)
	response, body := request(t, host, "GET", "/", nil)
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("data:font/woff2;base64,")) || !bytes.Contains(body, []byte("data:image/x-icon;base64,")) {
		t.Fatal("page is not self contained")
	}
	if !strings.Contains(response.Header.Get("Content-Security-Policy"), "script-src 'sha256-") {
		t.Fatal("missing CSP hashes")
	}
	for _, path := range []string{"/script.js", "/style.css", "/unknown"} {
		res, raw := request(t, host, "GET", path, nil)
		requireCode(t, res, raw, 404, "NOT_FOUND")
	}
	req, _ := http.NewRequest("GET", host.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := host.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := io.ReadAll(reader)
	reader.Close()
	if !bytes.Equal(decoded, body) {
		t.Fatal("gzip mismatch")
	}
	req, _ = http.NewRequest("GET", host.URL+"/", nil)
	req.Header.Set("If-None-Match", response.Header.Get("ETag"))
	res, err = host.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 304 {
		t.Fatal("missing conditional page response")
	}
	res, raw := request(t, host, "HEAD", "/", nil)
	if len(raw) != 0 || res.StatusCode != 200 {
		t.Fatal("invalid HEAD")
	}
}
func TestDownloadCacheAndCounting(t *testing.T) {
	var calls atomic.Int64
	app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("public_key") != "https://disk.yandex.ru/d/"+testID {
			t.Error("incorrect public key")
		}
		jsonBody(w, map[string]any{"href": testHref})
	})
	response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
	requireCode(t, response, raw, 200, "")
	var result struct {
		Href      string
		ExpiresAt int64
	}
	_ = json.Unmarshal(raw, &result)
	if result.Href != testHref || result.ExpiresAt != 1793347985000 {
		t.Fatalf("bad result %s", raw)
	}
	response, raw = request(t, host, "GET", "/d/"+testID, nil)
	if response.StatusCode != 302 || response.Header.Get("Location") != testHref {
		t.Fatal("missing redirect")
	}
	response, _ = request(t, host, "HEAD", "/api/download/d/"+testID, nil)
	if response.Header.Get("X-Converted-Links") != "2" || counterValue(t, app) != 2 || calls.Load() != 1 {
		t.Fatal("cache or HEAD counted incorrectly")
	}
	var group sync.WaitGroup
	for range 30 {
		group.Add(1)
		go func() {
			defer group.Done()
			res, err := host.Client().Get(host.URL + "/api/download/d/" + testID)
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}()
	}
	group.Wait()
	if counterValue(t, app) != 5 || calls.Load() != 1 {
		t.Fatal("counter/request limits did not bound concurrent requests")
	}
}
func TestURLExpiry(t *testing.T) {
	for value, expected := range map[string]int64{"6ae45191": 1793347985000, "6ae452e2": 1793348322000, "6AE452E2": 1793348322000} {
		deadline, ok := downloadExpiry("https://downloader.disk.yandex.ru/disk/signature/" + value + "/token")
		if !ok || deadline.UnixMilli() != expected {
			t.Fatal(value, deadline)
		}
	}
	var clock atomic.Int64
	clock.Store(testNow.UnixMilli())
	var expiry atomic.Int64
	expiry.Store(1793347985000)
	var calls atomic.Int64
	app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonBody(w, map[string]string{"href": fmt.Sprintf("https://downloader.disk.yandex.ru/disk/signature/%x/token", expiry.Load()/1000)})
	})
	app.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	request(t, host, "GET", "/api/download/d/"+testID, nil)
	clock.Add(int64(time.Hour / time.Millisecond))
	request(t, host, "GET", "/api/download/d/"+testID, nil)
	if calls.Load() != 1 {
		t.Fatal("cache expired after one hour")
	}
	clock.Store(expiry.Load())
	expiry.Add(337000)
	response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
	requireCode(t, response, raw, 200, "")
	if calls.Load() != 2 {
		t.Fatal("cache outlived URL")
	}
	clock.Store(expiry.Load())
	response, raw = request(t, host, "GET", "/api/download/d/"+testID, nil)
	requireCode(t, response, raw, 502, "EXPIRED_RESPONSE")
	if counterValue(t, app) != 3 {
		t.Fatal("expired response counted")
	}
}
func TestListsAndPathEncoding(t *testing.T) {
	app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/Папка & фото/file.txt" || len(r.URL.Query()) != 2 {
			t.Error(r.URL.String())
		}
		jsonBody(w, map[string]any{"_embedded": map[string]any{"items": []any{map[string]any{"name": "file.txt", "type": "file", "size": 12}}}})
	})
	response, raw := request(t, host, "GET", "/list/i/"+testID+"/%D0%9F%D0%B0%D0%BF%D0%BA%D0%B0%20%26%20%D1%84%D0%BE%D1%82%D0%BE/file.txt?ignored=yes", nil)
	requireCode(t, response, raw, 200, "")
	if !bytes.Contains(raw, []byte(`"size":12`)) || counterValue(t, app) != 0 {
		t.Fatal("listing counted or altered")
	}
}
func TestUpstreamErrors(t *testing.T) {
	for _, status := range []int{400, 403, 404, 429, 500, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("not JSON"))
			})
			response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
			expected := status
			if status >= 500 {
				expected = 502
			}
			requireCode(t, response, raw, expected, "UPSTREAM_ERROR")
			if counterValue(t, app) != 0 {
				t.Fatal("error counted")
			}
		})
	}
	for _, body := range []string{`{}`, `null`, `{"href":"javascript:alert(1)"}`, `{"href":"http://example.com"}`, `{"href":"https://user:pass@example.com"}`, `invalid JSON`} {
		t.Run(body, func(t *testing.T) {
			app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
			if response.StatusCode != 502 || counterValue(t, app) != 0 {
				t.Fatalf("%d %s", response.StatusCode, raw)
			}
		})
	}
}
func TestTimeoutAndValidation(t *testing.T) {
	app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	app.timeout = 20 * time.Millisecond
	response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
	requireCode(t, response, raw, 504, "UPSTREAM_TIMEOUT")
	for _, id := range []string{"short", strings.Repeat("a", 13), strings.Repeat("a", 15)} {
		response, raw = request(t, host, "GET", "/api/download/d/"+id, nil)
		requireCode(t, response, raw, 400, "INVALID_PUBLIC_KEY")
	}
}
func TestSingleFileFolderFallback(t *testing.T) {
	for _, total := range []int{1, 2} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			var calls atomic.Int64
			app, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if strings.HasSuffix(r.URL.Path, "/resources") {
					jsonBody(w, map[string]any{"type": "dir", "_embedded": map[string]any{"total": total, "items": []any{map[string]any{"type": "file", "name": "image & file.img", "path": "/image & file.img"}}}})
					return
				}
				if r.URL.Query().Get("path") == "/image & file.img" {
					jsonBody(w, map[string]string{"href": testHref})
					return
				}
				w.WriteHeader(500)
			})
			response, raw := request(t, host, "GET", "/api/download/d/"+testID, nil)
			if total == 1 {
				requireCode(t, response, raw, 200, "")
				request(t, host, "GET", "/api/download/d/"+testID, nil)
				if calls.Load() != 3 || counterValue(t, app) != 2 {
					t.Fatal("fallback not cached")
				}
			} else {
				requireCode(t, response, raw, 502, "FOLDER_DOWNLOAD_UNAVAILABLE")
				if counterValue(t, app) != 0 {
					t.Fatal("folder failure counted")
				}
			}
		})
	}
}
func TestBrowserCacheReports(t *testing.T) {
	app, host := fixture(t, nil)
	receipt, err := app.issueReceipt("/d/"+testID, testHref, 1793347985000)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"publicPath": "/d/" + testID, "href": testHref, "expiresAt": int64(1793347985000), "receipt": receipt}
	for count := int64(1); count <= 2; count++ {
		response, raw := request(t, host, "POST", "/api/stats/cache-hit", payload)
		requireCode(t, response, raw, 200, "")
		var stats statistics
		_ = json.Unmarshal(raw, &stats)
		payload["receipt"] = stats.Receipt
		if counterValue(t, app) != count {
			t.Fatal("hit not counted")
		}
	}
	for _, invalid := range []map[string]any{{"publicPath": "/d/short", "href": testHref, "expiresAt": 1793347985000}, {"publicPath": "/d/" + testID, "href": "https://evil.test/file", "expiresAt": 1793347985000}, {"publicPath": "/d/" + testID, "href": testHref, "expiresAt": 0}, {"publicPath": "/d/" + testID, "href": testHref, "expiresAt": 1793347986000}} {
		response, raw := request(t, host, "POST", "/api/stats/cache-hit", invalid)
		if response.StatusCode != 400 {
			t.Fatalf("%s", raw)
		}
	}
	response, _ := request(t, host, "POST", "/api/stats/cache-hit", map[string]string{"padding": strings.Repeat("x", 9000)})
	if response.StatusCode != 413 {
		t.Fatal("missing body limit")
	}
	if counterValue(t, app) != 2 {
		t.Fatal("invalid report counted")
	}
}
func TestSQLitePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "counter.sqlite")
	first, err := openCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.increment(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.close()
	second, err := openCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	stats, err := second.current(context.Background())
	if err != nil || stats.ConvertedLinks != 1 {
		t.Fatal(stats, err)
	}
	var group sync.WaitGroup
	for range 50 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := second.increment(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	stats, _ = second.current(context.Background())
	if stats.ConvertedLinks != 51 {
		t.Fatal(stats)
	}
}
func TestCacheBound(t *testing.T) {
	cache := newLinkCache()
	for i := range 257 {
		cache.set(cacheEntry{key: strconv.Itoa(i), expires: time.Now().Add(time.Hour)})
	}
	if _, ok := cache.get("0", time.Now()); ok {
		t.Fatal("cache exceeded bound")
	}
	if _, ok := cache.get("256", time.Now()); !ok {
		t.Fatal("latest evicted")
	}
}
func socketRead(t *testing.T, conn *websocket.Conn, accept func(statsMessage) bool) statsMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for range 10 {
		var result statsMessage
		if err := wsjson.Read(ctx, conn, &result); err != nil {
			t.Fatal(err)
		}
		if accept(result) {
			return result
		}
	}
	t.Fatal("message not received")
	return statsMessage{}
}
func TestWebSocketDuplex(t *testing.T) {
	app, host := fixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func() *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http")+"/api/stats/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		socketRead(t, conn, func(s statsMessage) bool { return s.Type == "stats" && s.ConvertedLinks == 0 })
		return conn
	}
	first, second := connect(), connect()
	if err := wsjson.Write(ctx, first, map[string]any{"type": "get", "id": 1}); err != nil {
		t.Fatal(err)
	}
	socketRead(t, first, func(s statsMessage) bool { return s.ID == 1 && s.ConvertedLinks == 0 })
	receipt, err := app.issueReceipt("/d/"+testID, testHref, 1793347985000)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, first, map[string]any{"receipt": receipt, "type": "cache-hit", "id": 2, "publicPath": "/d/" + testID, "href": testHref, "expiresAt": 1793347985000}); err != nil {
		t.Fatal(err)
	}
	socketRead(t, first, func(s statsMessage) bool { return s.ID == 2 && s.Type == "stats" && s.ConvertedLinks == 1 })
	socketRead(t, second, func(s statsMessage) bool { return s.ConvertedLinks == 1 })
	_ = wsjson.Write(ctx, first, map[string]any{"type": "cache-hit", "id": 3, "publicPath": "/d/short", "href": testHref, "expiresAt": 1793347985000})
	socketRead(t, first, func(s statsMessage) bool { return s.ID == 3 && s.Type == "error" && s.Error == "INVALID_CACHE_HIT" })
	if counterValue(t, app) != 1 {
		t.Fatal("invalid WS hit counted")
	}
}
func TestWebSocketOrigin(t *testing.T) {
	_, host := fixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http")+"/api/stats/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.test"}}})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil || response.StatusCode != 403 {
		t.Fatal("cross-origin socket accepted", err)
	}
}
func TestLegacyEventsRemoved(t *testing.T) {
	_, host := fixture(t, nil)
	res, body := request(t, host, "GET", "/api/stats/events", nil)
	requireCode(t, res, body, 404, "NOT_FOUND")
}
