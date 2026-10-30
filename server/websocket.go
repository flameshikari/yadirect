package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type statsCommand struct {
	Type string `json:"type"`
	ID   uint64 `json:"id"`
	cacheHitReport
}
type statsMessage struct {
	Type           string `json:"type"`
	ID             uint64 `json:"id,omitempty"`
	ConvertedLinks int64  `json:"convertedLinks"`
	Error          string `json:"error,omitempty"`
	Message        string `json:"message,omitempty"`
	Receipt        string `json:"receipt,omitempty"`
}

func (s *server) websocket(w http.ResponseWriter, r *http.Request) error {
	ip := s.clientIP(r)
	release, ok := s.guard.stream(ip, s.now())
	if !ok {
		return failure(429, "TOO_MANY_CONNECTIONS", "Too many statistics connections.")
	}
	defer release()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8192)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	commands := make(chan statsCommand, 8)
	readErrors := make(chan error, 1)
	go func() {
		for {
			var command statsCommand
			if err := wsjson.Read(ctx, conn, &command); err != nil {
				readErrors <- err
				return
			}
			if !s.guard.allow(ip, "command", s.now()) {
				readErrors <- failure(429, "RATE_LIMITED", "Too many commands")
				_ = conn.CloseNow()
				return
			}
			select {
			case commands <- command:
			case <-ctx.Done():
				return
			}
		}
	}()
	updates, unsubscribe := s.counter.subscribe()
	defer unsubscribe()
	send := func(message statsMessage) error {
		writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return wsjson.Write(writeCtx, conn, message)
	}
	initial, err := s.counter.current(ctx)
	if err != nil {
		return nil
	}
	if send(statsMessage{Type: "stats", ConvertedLinks: initial.ConvertedLinks}) != nil {
		return nil
	}
	// Ping frames detect a broken socket; they do not query the API or counter.
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.streams.Done():
			return nil
		case <-readErrors:
			return nil
		case stats := <-updates:
			if send(statsMessage{Type: "stats", ConvertedLinks: stats.ConvertedLinks, Receipt: stats.Receipt}) != nil {
				return nil
			}
		case command := <-commands:
			var stats statistics
			var err error
			commandCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			switch command.Type {
			case "get":
				stats, err = s.counter.current(commandCtx)
			case "cache-hit":
				stats, err = s.recordCacheHit(commandCtx, command.cacheHitReport, ip)
			default:
				err = failure(400, "INVALID_COMMAND", "Unknown statistics command.")
			}
			stop()
			message := statsMessage{Type: "stats", ID: command.ID, ConvertedLinks: stats.ConvertedLinks, Receipt: stats.Receipt}
			if err != nil {
				var api *apiError
				if !errors.As(err, &api) {
					api = &apiError{500, "INTERNAL_ERROR", "Could not update statistics."}
				}
				message = statsMessage{Type: "error", ID: command.ID, Error: api.code, Message: api.message}
			}
			if send(message) != nil {
				return nil
			}
		case <-ticker.C:
			pingCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			stop()
			if err != nil {
				return nil
			}
		}
	}
}
