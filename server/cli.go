package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

type options struct {
	port                          int
	host, database                string
	noCache, counter, showVersion bool
}

func parseOptions(args []string, getenv func(string) string, output io.Writer) (options, error) {
	o := options{port: 8080, host: "0.0.0.0", database: "data/yadirect.sqlite"}
	flags := flag.NewFlagSet("yadirect", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprint(output, `Usage: yadirect [OPTIONS]

A direct download link server for Yandex.Disk

OPTIONS
  -p, --port PORT        listen port (default: 8080; env: PORT)
  -H, --host ADDR        bind address (default: 0.0.0.0; env: HOST)
  -c, --counter         show the hit counter (disabled by default)
      --database FILE   counter SQLite file; requires -c/--counter
                        (default: data/yadirect.sqlite; env: SQLITE_PATH)
  -n, --no-cache         disable server link caching
  -v, --version          print version and exit
  -h, --help             show this help

ENVIRONMENT
  TRUSTED_PROXIES        comma-separated proxy IPs/CIDRs; uses X-Forwarded-For
                        when set; otherwise X-Real-IP is trusted from
                        10.0.0.0/8, 172.16.0.0/12 and 192.168.0.0/16
`)
	}
	for _, name := range []string{"port", "p"} {
		flags.IntVar(&o.port, name, 8080, "")
	}
	for _, name := range []string{"host", "H"} {
		flags.StringVar(&o.host, name, o.host, "")
	}
	flags.StringVar(&o.database, "database", o.database, "")
	for _, name := range []string{"counter", "c"} {
		flags.BoolVar(&o.counter, name, false, "")
	}
	for _, name := range []string{"no-cache", "n"} {
		flags.BoolVar(&o.noCache, name, false, "")
	}
	for _, name := range []string{"version", "v"} {
		flags.BoolVar(&o.showVersion, name, false, "")
	}
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if flags.NArg() != 0 {
		return o, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	if o.showVersion {
		return o, nil
	}
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if value := getenv("PORT"); value != "" && !explicit["port"] && !explicit["p"] {
		number, err := strconv.Atoi(value)
		if err != nil {
			return o, fmt.Errorf("PORT must be an integer")
		}
		o.port = number
	}
	if value := getenv("HOST"); value != "" && !explicit["host"] && !explicit["H"] {
		o.host = value
	}
	if value := getenv("SQLITE_PATH"); value != "" && !explicit["database"] {
		o.database = value
	}
	if explicit["database"] && !o.counter {
		return o, fmt.Errorf("--database requires -c/--counter")
	}
	if o.port < 0 || o.port > 65535 {
		return o, fmt.Errorf("port must be between 0 and 65535")
	}
	return o, nil
}

func logAddresses(host, port string) {
	log.Printf("Yadirect %s", version)
	if host == "" || host == "0.0.0.0" || host == "::" {
		addresses, _ := net.InterfaceAddrs()
		for _, a := range addresses {
			if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
				log.Printf("  http://%s", net.JoinHostPort(ip.IP.String(), port))
			}
		}
		host = "127.0.0.1"
	}
	log.Printf("  http://%s", net.JoinHostPort(host, port))
}

type loggedResponse struct {
	http.ResponseWriter
	status int
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *loggedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *loggedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func (w *loggedResponse) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *loggedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = 101
	}
	return conn, rw, err
}
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		response := &loggedResponse{ResponseWriter: w}
		defer func() {
			status := response.status
			if status == 0 {
				status = 200
			}
			log.Printf("%s %s %d %s", r.Method, r.URL.EscapedPath(), status, time.Since(start).Round(time.Microsecond))
		}()
		next.ServeHTTP(response, r)
	})
}
