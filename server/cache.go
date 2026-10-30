package main

import (
	"container/list"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const fallbackTTL = time.Hour

var publicKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{14}$`)
var downloadHostPattern = regexp.MustCompile(`^downloader\.disk\.yandex\.(ru|com|net)$`)
var expiryPattern = regexp.MustCompile(`(?i)^/disk/[^/]+/([0-9a-f]{8})/[^/]+`)

func downloadExpiry(href string) (time.Time, bool) {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "https" || u.User != nil || !downloadHostPattern.MatchString(u.Hostname()) {
		return time.Time{}, false
	}
	match := expiryPattern.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(match[1], 16, 64)
	return time.Unix(seconds, 0), err == nil
}
func validDownload(href string) bool {
	u, err := url.Parse(href)
	return err == nil && u.Scheme == "https" && downloadHostPattern.MatchString(u.Hostname()) && u.Port() == "" && u.User == nil && len(href) <= 8192 && !strings.ContainsAny(href, "\r\n")
}

type cacheEntry struct {
	key     string
	href    string
	items   json.RawMessage
	expires time.Time
}
type linkCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
}

func newLinkCache() *linkCache {
	return &linkCache{entries: make(map[string]*list.Element), order: list.New()}
}
func (c *linkCache) get(key string, now time.Time) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return cacheEntry{}, false
	}
	entry := element.Value.(cacheEntry)
	if !entry.expires.After(now) {
		delete(c.entries, key)
		c.order.Remove(element)
		return cacheEntry{}, false
	}
	return entry, true
}
func (c *linkCache) set(entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[entry.key]; ok {
		element.Value = entry
		return
	}
	if len(c.entries) >= 256 {
		oldest := c.order.Front()
		delete(c.entries, oldest.Value.(cacheEntry).key)
		c.order.Remove(oldest)
	}
	c.entries[entry.key] = c.order.PushBack(entry)
}
