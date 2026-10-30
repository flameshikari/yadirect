package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type statistics struct {
	ConvertedLinks int64  `json:"convertedLinks"`
	Receipt        string `json:"receipt,omitempty"`
}
type counter struct {
	db        *sql.DB
	key       []byte
	mu        sync.Mutex
	listeners map[chan statistics]struct{}
}

func openCounter(path string) (*counter, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return nil, err
		}
		if err = file.Chmod(0600); err != nil {
			file.Close()
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Chmod(path+suffix, 0600); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, query := range []string{
		"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL", "PRAGMA max_page_count = 16384",
		`CREATE TABLE IF NOT EXISTS counter_security (id INTEGER PRIMARY KEY CHECK(id=1), receipt_key BLOB NOT NULL CHECK(length(receipt_key)=32))`,
		`CREATE TABLE IF NOT EXISTS used_receipts (token BLOB PRIMARY KEY, expires_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS used_receipts_expiry ON used_receipts(expires_at)`,
		`CREATE TABLE IF NOT EXISTS conversion_counter (id INTEGER PRIMARY KEY CHECK (id = 1), converted_links INTEGER NOT NULL DEFAULT 0 CHECK (converted_links >= 0))`,
		"INSERT OR IGNORE INTO conversion_counter (id, converted_links) VALUES (1, 0)",
	} {
		if _, err = db.Exec(query); err != nil {
			db.Close()
			return nil, err
		}
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err == nil {
		_, err = db.Exec("INSERT OR IGNORE INTO counter_security(id,receipt_key) VALUES(1,?)", key)
	}
	if err == nil {
		err = db.QueryRow("SELECT receipt_key FROM counter_security WHERE id=1").Scan(&key)
	}
	if err == nil && path != ":memory:" {
		err = os.Chmod(path, 0600)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &counter{db: db, key: key, listeners: make(map[chan statistics]struct{})}, nil
}

func (c *counter) current(ctx context.Context) (statistics, error) {
	if c == nil {
		return statistics{}, nil
	}
	var result statistics
	err := c.db.QueryRowContext(ctx, "SELECT converted_links FROM conversion_counter WHERE id = 1").Scan(&result.ConvertedLinks)
	return result, err
}
func (c *counter) increment(ctx context.Context) (statistics, error) {
	if c == nil {
		return statistics{}, nil
	}
	var result statistics
	err := c.db.QueryRowContext(ctx, "UPDATE conversion_counter SET converted_links = converted_links + 1 WHERE id = 1 AND converted_links < 9007199254740991 RETURNING converted_links").Scan(&result.ConvertedLinks)
	if errors.Is(err, sql.ErrNoRows) {
		return c.current(ctx)
	}
	if err != nil {
		return result, err
	}
	c.publish(result)
	return result, nil
}
func (c *counter) publish(result statistics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for listener := range c.listeners {
		select {
		case previous := <-listener:
			if previous.ConvertedLinks > result.ConvertedLinks {
				listener <- previous
				continue
			}
		default:
		}
		listener <- result
	}
}
func (c *counter) subscribe() (<-chan statistics, func()) {
	listener := make(chan statistics, 1)
	c.mu.Lock()
	c.listeners[listener] = struct{}{}
	c.mu.Unlock()
	return listener, func() { c.mu.Lock(); delete(c.listeners, listener); c.mu.Unlock() }
}
func (c *counter) close() error { return c.db.Close() }

// Consume each signed receipt once, atomically with its optional counter increment.
func (c *counter) consume(ctx context.Context, token [32]byte, expiry, now time.Time, count bool) (statistics, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return statistics{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM used_receipts WHERE expires_at<=?", now.UnixMilli()); err != nil {
		return statistics{}, err
	}
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM used_receipts").Scan(&total); err != nil {
		return statistics{}, err
	}
	if total >= 100000 {
		return statistics{}, failure(429, "COUNTER_BUSY", "The counter is temporarily busy.")
	}
	inserted, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO used_receipts(token,expires_at) VALUES(?,?)", token[:], expiry.UnixMilli())
	if err != nil {
		return statistics{}, err
	}
	rows, _ := inserted.RowsAffected()
	if rows == 0 {
		return statistics{}, failure(409, "RECEIPT_REPLAYED", "This download receipt has already been used.")
	}
	changed := false
	if count {
		update, err := tx.ExecContext(ctx, "UPDATE conversion_counter SET converted_links=converted_links+1 WHERE id=1 AND converted_links<9007199254740991")
		if err != nil {
			return statistics{}, err
		}
		n, _ := update.RowsAffected()
		changed = n > 0
	}
	var stats statistics
	if err = tx.QueryRowContext(ctx, "SELECT converted_links FROM conversion_counter WHERE id=1").Scan(&stats.ConvertedLinks); err != nil {
		return statistics{}, err
	}
	if err = tx.Commit(); err != nil {
		return statistics{}, err
	}
	if changed {
		c.publish(stats)
	}
	return stats, nil
}
