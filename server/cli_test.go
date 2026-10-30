package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCLIOptions(t *testing.T) {
	env := func(key string) string {
		return map[string]string{"PORT": "4321", "HOST": "127.0.0.2", "SQLITE_PATH": "custom.sqlite"}[key]
	}
	o, err := parseOptions([]string{"-p", "1234", "-H", "127.0.0.1", "--database", "other.sqlite", "-c", "-n"}, env, io.Discard)
	if err != nil || o.port != 1234 || o.host != "127.0.0.1" || o.database != "other.sqlite" || !o.counter || !o.noCache {
		t.Fatalf("options: %+v, %v", o, err)
	}
	o, err = parseOptions(nil, env, io.Discard)
	if err != nil || o.port != 4321 || o.host != "127.0.0.2" || o.database != "custom.sqlite" {
		t.Fatalf("env: %+v, %v", o, err)
	}
	for _, args := range [][]string{{"--port", "65536"}, {"--port", "-1"}, {"extra"}, {"--unknown"}, {"--healthcheck"}, {"--root", "public"}, {"-d", "other.sqlite"}, {"--database", "other.sqlite"}} {
		if _, err := parseOptions(args, env, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	var help bytes.Buffer
	if _, err := parseOptions([]string{"--help"}, env, &help); !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), "Usage: yadirect [OPTIONS]") {
		t.Fatalf("help: %v %s", err, &help)
	}
}

func TestNoCacheBypassesExistingEntries(t *testing.T) {
	calls := 0
	s, host := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		jsonBody(w, map[string]string{"href": testHref})
	})
	path := "/api/download/d/" + testID
	request(t, host, "GET", path, nil)
	request(t, host, "GET", path, nil)
	if calls != 1 {
		t.Fatalf("cache calls: %d", calls)
	}
	s.noCache = true
	request(t, host, "GET", path, nil)
	request(t, host, "GET", path, nil)
	if calls != 3 {
		t.Fatalf("uncached calls: %d", calls)
	}
	if counterValue(t, s) != 4 {
		t.Fatal("cache setting affected successful request counting")
	}
}

func TestCounterDefaults(t *testing.T) {
	o, err := parseOptions(nil, func(string) string { return "" }, io.Discard)
	if err != nil || o.port != 8080 || o.counter {
		t.Fatalf("defaults: %+v %v", o, err)
	}
	for _, flag := range []string{"-c", "--counter"} {
		o, err = parseOptions([]string{flag, "--database", "custom.sqlite"}, func(string) string { return "" }, io.Discard)
		if err != nil || !o.counter || o.database != "custom.sqlite" {
			t.Fatalf("counter options: %+v %v", o, err)
		}
	}
}

func TestCounterDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { jsonBody(w, map[string]string{"href": testHref}) }))
	defer upstream.Close()
	s := newServer(nil)
	defer s.stopStreams()
	s.apiBase = upstream.URL
	s.client = upstream.Client()
	s.now = func() time.Time { return testNow }
	host := httptest.NewServer(s)
	defer host.Close()
	res, body := request(t, host, "GET", "/", nil)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte(`name="yadirect-counter" content="disabled"`)) {
		t.Fatal("counter enabled in default page")
	}
	for _, path := range []string{"/api/stats", "/api/stats/ws", "/api/stats/cache-hit", "/api/stats/events"} {
		res, body = request(t, host, "GET", path, nil)
		requireCode(t, res, body, 404, "COUNTER_DISABLED")
	}
	res, body = request(t, host, "GET", "/api/download/d/"+testID, nil)
	if res.StatusCode != 200 || res.Header.Get("X-Converted-Links") != "" {
		t.Fatalf("disabled counter interferes with conversion: %d %s", res.StatusCode, body)
	}
}

func TestCounterEnabledPage(t *testing.T) {
	_, host := fixture(t, nil)
	_, body := request(t, host, "GET", "/", nil)
	if !bytes.Contains(body, []byte(`name="yadirect-counter" content="enabled"`)) {
		t.Fatal("counter missing from enabled page")
	}
}
