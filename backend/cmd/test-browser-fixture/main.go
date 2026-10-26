//go:build e2e

// test-browser-fixture is an isolated validation responder, never an application
// runtime. Account observations read PostgreSQL independently of HTTP DTOs.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

const (
	fixtureUsername = "isolated-browser-proxy"
	fixturePassword = "isolated-browser-proxy-password"
)

type observations struct {
	Connections       map[string]int `json:"connections"`
	HeldStarted       bool           `json:"held_started"`
	CredentialsLeaked bool           `json:"credentials_leaked"`
	DirectConnections int            `json:"direct_connections"`
}

type fixture struct {
	mu       sync.Mutex
	state    observations
	released chan struct{}
	db       *sql.DB
	cert     tls.Certificate
	ports    map[string]int
}

func (f *fixture) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.released != nil {
		select {
		case <-f.released:
		default:
			close(f.released)
		}
	}
	f.released = make(chan struct{})
	f.state = observations{Connections: map[string]int{"healthy": 0, "refused": 0, "held": 0, "repaired": 0}}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fixture) control(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/control/info":
		writeJSON(w, map[string]any{"host": "127.0.0.1", "ports": f.ports, "username": fixtureUsername, "password": fixturePassword})
	case r.Method == http.MethodPost && r.URL.Path == "/control/reset":
		f.reset()
		writeJSON(w, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && r.URL.Path == "/control/release":
		f.mu.Lock()
		select {
		case <-f.released:
		default:
			close(f.released)
		}
		f.mu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/control/state":
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, f.state)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/control/accounts/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/control/accounts/"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid account identifier", http.StatusBadRequest)
			return
		}
		var name, platform, accountType string
		var proxyID sql.NullInt64
		var credentials []byte
		err = f.db.QueryRowContext(r.Context(), `SELECT name, platform, type, proxy_id, credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL`, id).
			Scan(&name, &platform, &accountType, &proxyID, &credentials)
		if err != nil {
			http.Error(w, "account observation unavailable", http.StatusNotFound)
			return
		}
		var savedProxyID any
		if proxyID.Valid {
			savedProxyID = proxyID.Int64
		}
		writeJSON(w, map[string]any{"id": id, "name": name, "platform": platform, "type": accountType, "proxy_id": savedProxyID, "credentials": json.RawMessage(credentials)})
	default:
		http.NotFound(w, r)
	}
}

func isProbeHost(host string) bool {
	host, _, _ = strings.Cut(host, ":")
	return host == "ip-api.com" || host == "api64.ipify.org"
}

func (f *fixture) observeHeaders(r *http.Request, direct bool) {
	leaked := direct && r.Header.Get("Proxy-Authorization") != ""
	if isProbeHost(r.Host) {
		for _, name := range []string{"Authorization", "X-Api-Key", "Api-Key", "Cookie"} {
			leaked = leaked || r.Header.Get(name) != ""
		}
	}
	if leaked {
		f.mu.Lock()
		f.state.CredentialsLeaked = true
		f.mu.Unlock()
	}
}

func (f *fixture) respond(w http.ResponseWriter, r *http.Request, route string) {
	f.observeHeaders(r, false)
	if !isProbeHost(r.Host) {
		// Background production import/privacy requests stay local. They are
		// outside the management probe scenario and never grant authorization.
		writeJSON(w, map[string]any{"data": []any{}, "models": []any{}, "object": "list", "training_allowed": false})
		return
	}
	country := "NL"
	if route == "held" {
		country = "STALE"
	}
	writeJSON(w, map[string]any{"status": "success", "query": "203.0.113.45", "ip": "203.0.113.45", "country": country, "countryCode": "NL", "regionName": "Fixture region", "city": "Fixture city"})
}

func (f *fixture) proxy(route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.state.Connections[route]++
		release := f.released
		f.mu.Unlock()
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixtureUsername+":"+fixturePassword))
		if r.Header.Get("Proxy-Authorization") != want {
			w.Header().Set("Proxy-Authenticate", `Basic realm="isolated-browser"`)
			http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
			return
		}
		if route == "refused" {
			http.Error(w, "isolated proxy refused request", http.StatusServiceUnavailable)
			return
		}
		if route == "held" && isProbeHost(r.Host) {
			f.mu.Lock()
			f.state.HeldStarted = true
			f.mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if r.Method != http.MethodConnect {
			f.respond(w, r, route)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "CONNECT unavailable", http.StatusInternalServerError)
			return
		}
		conn, buffered, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if buffered.Flush() != nil {
			return
		}
		secure := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{f.cert}})
		if secure.HandshakeContext(r.Context()) != nil {
			return
		}
		// A one-connection HTTP server serves the TLS tunnel using the same
		// isolated responder, without dialing the destination or disabling TLS.
		listener := &singleListener{conn: secure, done: make(chan struct{})}
		server := &http.Server{Handler: http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) { f.respond(out, request, route) }), ReadHeaderTimeout: 5 * time.Second}
		_ = server.Serve(listener)
	})
}

type singleListener struct {
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func (l *singleListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return &closingConn{Conn: conn, done: l.done}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *singleListener) Close() error   { return l.conn.Close() }
func (l *singleListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closingConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (c *closingConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}

type observedListener struct {
	net.Listener
	accepted func()
}

func (l *observedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepted()
	}
	return conn, err
}

func certificate(directory string, hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable Sub2API browser fixture CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(filepath.Join(directory, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		return tls.Certificate{}, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Disposable Sub2API network responder"}, DNSNames: hosts, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func main() {
	controlPort := flag.Int("control-port", 18091, "loopback control port")
	proxyBasePort := flag.Int("proxy-base-port", 18110, "first of four loopback proxy ports")
	caDir := flag.String("ca-dir", "", "disposable CA output directory")
	hosts := flag.String("hosts", "", "comma-separated isolated destination hosts")
	initializeDB := flag.String("init-database", "", "create an isolated browser test database")
	dropDB := flag.String("drop-database", "", "remove an isolated browser test database")
	flag.Parse()
	if os.Getenv("SUB2API_E2E_ISOLATED") != "1" || os.Getenv("SUB2API_IN_VALIDATION") != "1" {
		log.Fatal("browser fixture requires an explicitly isolated validation container")
	}
	if _, err := os.Stat("/etc/sub2api-validation"); err != nil {
		log.Fatal("browser fixture requires the validation image marker")
	}
	db, err := sql.Open("postgres", os.Getenv("SUB2API_TEST_POSTGRES_DSN"))
	if err != nil {
		log.Fatal("invalid isolated PostgreSQL configuration")
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal("isolated PostgreSQL is unavailable")
	}
	if *initializeDB != "" || *dropDB != "" {
		name := *initializeDB
		if name == "" {
			name = *dropDB
		}
		if !regexp.MustCompile(`^sub2api_browser_[a-f0-9]{16}$`).MatchString(name) {
			log.Fatal("refusing database mutation outside the generated disposable browser namespace")
		}
		statement := `CREATE DATABASE "` + name + `"`
		if *dropDB != "" {
			statement = `DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			log.Fatal("isolated browser database lifecycle failed")
		}
		return
	}
	if *caDir == "" || *hosts == "" {
		log.Fatal("CA directory and isolated destination hosts are required")
	}
	cert, err := certificate(*caDir, strings.Split(*hosts, ","))
	if err != nil {
		log.Fatal("disposable fixture certificate creation failed")
	}
	f := &fixture{db: db, cert: cert, ports: make(map[string]int)}
	f.reset()
	var listeners []net.Listener
	start := func(address string, handler http.Handler, secure, observed bool) {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			log.Fatalf("isolated fixture listener unavailable: %s", address)
		}
		if observed {
			listener = &observedListener{Listener: listener, accepted: func() { f.mu.Lock(); f.state.DirectConnections++; f.mu.Unlock() }}
		}
		if secure {
			listener = tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
		}
		listeners = append(listeners, listener)
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = server.Serve(listener) }()
	}
	for index, name := range []string{"healthy", "refused", "held", "repaired"} {
		f.ports[name] = *proxyBasePort + index
		start(net.JoinHostPort("127.0.0.1", strconv.Itoa(*proxyBasePort+index)), f.proxy(name), false, false)
	}
	// Separate addresses make the geo-target counter measure accepted TCP
	// connections, including clients that never send an HTTP request.
	direct := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.observeHeaders(r, true)
		http.Error(w, "unexpected direct probe destination", http.StatusBadGateway)
	})
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.respond(w, r, "healthy") })
	for _, port := range []string{"80", "443"} {
		start("127.0.0.2:"+port, direct, port == "443", true)
		start("127.0.0.3:"+port, provider, port == "443", false)
	}
	start(net.JoinHostPort("127.0.0.1", strconv.Itoa(*controlPort)), http.HandlerFunc(f.control), false, false)
	fmt.Fprintln(os.Stdout, "isolated browser network fixture ready")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	<-signals
	f.mu.Lock()
	select {
	case <-f.released:
	default:
		close(f.released)
	}
	f.mu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
