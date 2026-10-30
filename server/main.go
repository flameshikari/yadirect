package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

//go:embed public/index.html
var embeddedPage []byte
var version = "dev"

func main() {
	options, err := parseOptions(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if options.showVersion {
		fmt.Println(version)
		return
	}
	var counter *counter
	if options.counter {
		counter, err = openCounter(options.database)
		if err != nil {
			log.Fatalf("open SQLite counter: %v", err)
		}
		defer counter.close()
		log.Printf("Counter database: %s", options.database)
	}
	app := newServer(counter)
	defer app.stopStreams()
	app.noCache = options.noCache
	app.trustedProxies, err = trustedProxyRanges(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		log.Fatal(err)
	}
	log.Print("Serving from embedded filesystem")
	if app.noCache {
		log.Print("Caching disabled")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(options.host, strconv.Itoa(options.port)))
	if err != nil {
		log.Fatal(err)
	}
	listener = newBoundedListener(listener, 256)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	logAddresses(options.host, port)
	httpServer := &http.Server{Handler: loggingMiddleware(app), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	failures := make(chan error, 1)
	go func() { failures <- httpServer.Serve(listener) }()
	select {
	case err := <-failures:
		if err != http.ErrServerClosed {
			log.Printf("HTTP server failed: %v", err)
			os.Exit(1)
		}
	case <-signals:
		app.stopStreams()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			_ = httpServer.Close()
		}
	}
}
