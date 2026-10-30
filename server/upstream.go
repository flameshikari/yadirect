package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string                    { return e.message }
func failure(status int, code, message string) error { return &apiError{status, code, message} }

type resource struct {
	Href     string `json:"href"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Embedded *struct {
		Total int               `json:"total"`
		Items []json.RawMessage `json:"items"`
	} `json:"_embedded"`
}

func (s *server) fetchResource(ctx context.Context, u *url.URL) (int, resource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, resource{}, err
	}
	response, err := s.client.Do(req)
	if err != nil {
		return 0, resource{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return response.StatusCode, resource{}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil {
		return 0, resource{}, err
	}
	if len(raw) > 4*1024*1024 {
		return 0, resource{}, errors.New("upstream response too large")
	}
	var body resource
	if err = json.Unmarshal(raw, &body); err != nil {
		return 0, resource{}, err
	}
	return response.StatusCode, body, nil
}

func upstreamFailure(status int) error {
	messages := map[int]string{
		400: "Yandex.Disk could not process this link or folder path.",
		403: "Access to this file is restricted or its download limit has been reached.",
		404: "This file was not found. Check that the link is public and still available.",
		429: "Yandex.Disk is receiving too many requests. Please try again later.",
	}
	if message, ok := messages[status]; ok {
		return failure(status, "UPSTREAM_ERROR", message)
	}
	return failure(502, "UPSTREAM_ERROR", "Yandex.Disk is temporarily unavailable. Please try again.")
}

func (s *server) resolveLink(ctx context.Context, kind, key, folder string, isList bool) (cacheEntry, error) {
	u, _ := url.Parse(s.apiBase + "/v1/disk/public/resources")
	if !isList {
		u.Path += "/download"
	}
	query := url.Values{"public_key": []string{"https://disk.yandex.ru/" + kind + "/" + key}}
	if folder != "" {
		query.Set("path", folder)
	}
	u.RawQuery = query.Encode()
	if entry, ok := s.cache.get(u.String(), s.now()); ok && !s.noCache {
		return entry, nil
	}
	select {
	case s.upstreamSlots <- struct{}{}:
		defer func() { <-s.upstreamSlots }()
	default:
		return cacheEntry{}, failure(503, "SERVER_BUSY", "Too many links are being processed. Please try again.")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	entry, err := s.fetchLink(ctx, u, folder, isList)
	if err != nil {
		var api *apiError
		if errors.As(err, &api) {
			return entry, err
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return entry, failure(504, "UPSTREAM_TIMEOUT", "Yandex.Disk took too long to respond. Please try again.")
		}
		return entry, failure(502, "UPSTREAM_UNAVAILABLE", "Could not read a response from Yandex.Disk. Please try again.")
	}
	entry.key = u.String()
	if !s.noCache {
		s.cache.set(entry)
	}
	return entry, nil
}
func (s *server) fetchLink(ctx context.Context, u *url.URL, folder string, isList bool) (cacheEntry, error) {
	status, body, err := s.fetchResource(ctx, u)
	if err != nil {
		return cacheEntry{}, err
	}
	isFolder := false
	if !isList && status >= 500 {
		metadataURL := *u
		metadataURL.Path = "/v1/disk/public/resources"
		query := metadataURL.Query()
		query.Set("limit", "2")
		metadataURL.RawQuery = query.Encode()
		metaStatus, meta, metaErr := s.fetchResource(ctx, &metadataURL)
		if metaErr != nil {
			return cacheEntry{}, metaErr
		}
		isFolder = metaStatus == 200 && meta.Type == "dir"
		if isFolder && meta.Embedded != nil && meta.Embedded.Total == 1 && len(meta.Embedded.Items) == 1 {
			var file resource
			if json.Unmarshal(meta.Embedded.Items[0], &file) == nil && file.Type == "file" && file.Name != "" {
				filePath := file.Path
				if !strings.HasPrefix(filePath, "/") {
					filePath = path.Join("/", folder, file.Name)
				}
				fileURL := *u
				query := fileURL.Query()
				query.Set("path", filePath)
				fileURL.RawQuery = query.Encode()
				status, body, err = s.fetchResource(ctx, &fileURL)
				if err != nil {
					return cacheEntry{}, err
				}
			}
		}
	}
	if status < 200 || status >= 300 {
		if isFolder && status >= 500 {
			return cacheEntry{}, failure(502, "FOLDER_DOWNLOAD_UNAVAILABLE", "Yandex.Disk could not prepare this folder archive. Try downloading individual files.")
		}
		return cacheEntry{}, upstreamFailure(status)
	}
	entry := cacheEntry{expires: s.now().Add(fallbackTTL)}
	if isList {
		if body.Embedded == nil || body.Embedded.Items == nil {
			return entry, failure(502, "INVALID_RESPONSE", "Yandex.Disk did not return a folder listing.")
		}
		entry.items, _ = json.Marshal(body.Embedded.Items)
		entry.expires = s.now().Add(time.Minute)
	} else {
		if !validDownload(body.Href) {
			return entry, failure(502, "INVALID_RESPONSE", "Yandex.Disk did not return a valid download link.")
		}
		entry.href = body.Href
		if deadline, ok := downloadExpiry(entry.href); ok {
			entry.expires = deadline
		}
		if !entry.expires.After(s.now()) {
			return entry, failure(502, "EXPIRED_RESPONSE", "Yandex.Disk returned an expired download link. Please try again.")
		}
	}
	return entry, nil
}
