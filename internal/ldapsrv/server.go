package ldapsrv

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const refreshEvery = 30 * time.Second

// Options wires the listener to the hub. Fetch may fail while factum2 is
// down; the last snapshot on disk keeps answering.
type Options struct {
	StatePath string
	// TLSCert and TLSKey are the worker hub certificate. LDAPS uses them
	// when the snapshot does not name a certificate from the store.
	TLSCert string
	TLSKey  string
	// LegoStorage is this worker's lego data directory (worker.paths
	// lego_storage). A chosen certificate is read from certificates/
	// under that directory.
	LegoStorage string
	Fetch       func(ctx context.Context) ([]byte, error)
}

// Config is the snapshot a worker persists and serves.
type Config struct {
	Enabled   bool   `json:"enabled"`
	Listen    string `json:"listen"`
	TLSListen string `json:"tls_listen,omitempty"`
	// TLSCertificate is the certificate-store name. TLSDomain is its
	// first domain, which is the lego file stem before sanitizing.
	// Both empty means LDAPS uses the worker hub certificate.
	TLSCertificate string      `json:"tls_certificate,omitempty"`
	TLSDomain      string      `json:"tls_domain,omitempty"`
	BaseDN         string      `json:"base_dn"`
	BindDN         string      `json:"bind_dn"`
	BindPassword   string      `json:"bind_password,omitempty"`
	Entries        []WireEntry `json:"entries"`
}

// WireEntry is one object in the snapshot. Password is a bcrypt hash.
// Members and MemberOf are the direct edges. The memberof attribute is
// the expanded list.
type WireEntry struct {
	DN         string              `json:"dn"`
	Attributes map[string][]string `json:"attributes"`
	Password   string              `json:"password,omitempty"`
	Members    []string            `json:"members,omitempty"`
	MemberOf   []string            `json:"member_of,omitempty"`
}

// Snapshot copies the directory into a config the worker can store.
func (d *Directory) Snapshot(enabled bool, listen, tlsListen, bindPassword string) Config {
	if d == nil {
		return Config{Enabled: enabled, Listen: listen, TLSListen: tlsListen}
	}
	cfg := Config{
		Enabled:      enabled,
		Listen:       listen,
		TLSListen:    tlsListen,
		BaseDN:       d.BaseDN,
		BindDN:       d.BindDN,
		BindPassword: bindPassword,
	}
	for _, e := range d.entries {
		cfg.Entries = append(cfg.Entries, WireEntry{
			DN:         e.DN,
			Attributes: e.Attrs,
			Password:   e.Password,
			Members:    e.DirectMembers,
			MemberOf:   e.DirectMemberOf,
		})
	}
	return cfg
}

// Directory rebuilds a snapshot into a searchable directory.
func (c Config) Directory() *Directory {
	base := strings.TrimSpace(c.BaseDN)
	if base == "" {
		base = DefaultBaseDN
	}
	d := &Directory{
		BaseDN:       base,
		BindDN:       c.BindDN,
		BindPassword: c.BindPassword,
		byDN:         map[string]*Entry{},
	}
	if d.BindDN == "" {
		d.BindDN = ServiceBindDN(base)
	}
	for _, w := range c.Entries {
		if strings.TrimSpace(w.DN) == "" {
			continue
		}
		attrs := map[string][]string{}
		for k, v := range w.Attributes {
			attrs[strings.ToLower(k)] = v
		}
		d.add(&Entry{
			DN:             w.DN,
			Attrs:          attrs,
			Password:       w.Password,
			DirectMembers:  w.Members,
			DirectMemberOf: w.MemberOf,
			operational:    map[string]bool{"memberof": true},
		})
	}
	if d.byDN[normalizeDN(base)] == nil {
		d.add(baseEntry(base))
	}
	return d
}

// Server answers LDAP. The snapshot is swapped as a whole.
type Server struct {
	opt  Options
	mu   sync.RWMutex
	dir  *Directory
	cfg  Config
	last []byte
}

// Run refreshes the snapshot and serves LDAP until ctx ends.
func Run(ctx context.Context, opt Options) error {
	s := &Server{opt: opt}
	s.loadCache()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.refreshLoop(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); s.listenLoop(ctx, false) }()
	wg.Add(1)
	go func() { defer wg.Done(); s.listenLoop(ctx, true) }()
	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Server) current() (Config, *Directory) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, s.dir
}

func (s *Server) refreshLoop(ctx context.Context) {
	s.refresh(ctx)
	t := time.NewTicker(refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		}
	}
}

func (s *Server) refresh(ctx context.Context) {
	if s.opt.Fetch == nil {
		return
	}
	body, err := s.opt.Fetch(ctx)
	if err != nil {
		slog.Debug("ldap config refresh failed", "err", err)
		return
	}
	s.mu.RLock()
	same := bytes.Equal(body, s.last)
	s.mu.RUnlock()
	if same {
		return
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		slog.Error("ldap config rejected", "err", err)
		return
	}
	if err := ValidateListen(cfg.Listen); err != nil || ValidateListen(cfg.TLSListen) != nil {
		slog.Error("ldap listen rejected")
		return
	}
	s.apply(cfg, body)
	if err := s.saveCache(cfg); err != nil {
		slog.Error("ldap config cache", "err", err)
	}
	slog.Info("ldap config applied", "enabled", cfg.Enabled, "entries", len(cfg.Entries))
}

func (s *Server) apply(cfg Config, body []byte) {
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = DefaultListen
	}
	dir := cfg.Directory()
	s.mu.Lock()
	s.cfg = cfg
	s.dir = dir
	s.last = append([]byte(nil), body...)
	s.mu.Unlock()
}

func (s *Server) loadCache() {
	if s.opt.StatePath == "" {
		return
	}
	b, err := os.ReadFile(s.opt.StatePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("ldap config cache unreadable", "err", err)
		}
		return
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		slog.Warn("ldap config cache rejected", "err", err)
		return
	}
	s.apply(cfg, b)
	slog.Info("ldap config loaded from disk", "enabled", cfg.Enabled)
}

func (s *Server) saveCache(cfg Config) error {
	if s.opt.StatePath == "" {
		return nil
	}
	dir := filepath.Dir(s.opt.StatePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := s.opt.StatePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.opt.StatePath)
}

func (s *Server) listenLoop(ctx context.Context, tlsListen bool) {
	var ln net.Listener
	var bound string
	defer func() {
		if ln != nil {
			ln.Close()
		}
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		cfg, _ := s.current()
		addr := ""
		if cfg.Enabled {
			addr = cfg.Listen
			if tlsListen {
				addr = strings.TrimSpace(cfg.TLSListen)
			}
		}
		if addr == "" {
			if ln != nil {
				ln.Close()
				ln = nil
				bound = ""
			}
			if !sleep(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if bound != addr {
			if ln != nil {
				ln.Close()
				ln = nil
			}
			var err error
			if tlsListen {
				ln, err = s.tlsListen(addr)
			} else {
				ln, err = net.Listen("tcp", addr)
			}
			if err != nil {
				slog.Error("ldap listen", "addr", addr, "tls", tlsListen, "err", err)
				if !sleep(ctx, 5*time.Second) {
					return
				}
				continue
			}
			bound = addr
			slog.Info("ldap listening", "addr", addr, "tls", tlsListen)
		}
		setAcceptDeadline(ln)
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go s.serveConn(ctx, conn)
	}
}

func setAcceptDeadline(ln net.Listener) {
	if d, ok := ln.(interface{ SetDeadline(time.Time) error }); ok {
		_ = d.SetDeadline(time.Now().Add(2 * time.Second))
	}
}

type deadlineListener struct {
	net.Listener
	tcp net.Listener
}

func (d deadlineListener) SetDeadline(t time.Time) error {
	x, ok := d.tcp.(interface{ SetDeadline(time.Time) error })
	if !ok {
		return nil
	}
	return x.SetDeadline(t)
}

func (s *Server) tlsListen(addr string) (net.Listener, error) {
	if _, err := s.tlsCertificate(); err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ln := tls.NewListener(tcp, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := s.tlsCertificate()
			if err != nil {
				return nil, err
			}
			return &cert, nil
		},
	})
	return deadlineListener{Listener: ln, tcp: tcp}, nil
}

// tlsCertificate loads the pair LDAPS should present. A store certificate
// is read from lego storage. Otherwise the worker hub pair is used.
func (s *Server) tlsCertificate() (tls.Certificate, error) {
	cfg, _ := s.current()
	if name := strings.TrimSpace(cfg.TLSCertificate); name != "" {
		stem, err := LegoCertStem(cfg.TLSDomain)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("ldaps certificate %s: %w", name, err)
		}
		root := strings.TrimSpace(s.opt.LegoStorage)
		if root == "" {
			return tls.Certificate{}, fmt.Errorf("ldaps certificate %s: lego storage is not configured", name)
		}
		crt := filepath.Join(root, "certificates", stem+".crt")
		key := filepath.Join(root, "certificates", stem+".key")
		pair, err := tls.LoadX509KeyPair(crt, key)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("ldaps certificate %s: %w", name, err)
		}
		return pair, nil
	}
	if strings.TrimSpace(s.opt.TLSCert) == "" || strings.TrimSpace(s.opt.TLSKey) == "" {
		return tls.Certificate{}, errors.New("ldaps: no certificate configured")
	}
	pair, err := tls.LoadX509KeyPair(s.opt.TLSCert, s.opt.TLSKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return pair, nil
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	st := &bindState{}
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		msg, err := readBER(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
				slog.Debug("ldap read", "err", err)
			}
			return
		}
		_, dir := s.current()
		if dir == nil {
			return
		}
		pkts, done := dispatch(dir, st, msg)
		for _, p := range pkts {
			_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := conn.Write(p); err != nil {
				return
			}
		}
		if done {
			return
		}
	}
}

type bindState struct {
	ok   bool
	anon bool
}

func dispatch(d *Directory, st *bindState, msg ber) ([][]byte, bool) {
	if msg.cls != 0 || msg.tag != 16 || len(msg.kids) < 2 {
		return nil, true
	}
	id64, err := msg.kids[0].integer()
	if err != nil {
		return nil, true
	}
	id := int(id64)
	op := msg.kids[1]
	if len(msg.kids) >= 3 && opControlCritical(msg.kids[2]) {
		return [][]byte{encodeMessage(id, encodeResult(opResultTag(op.tag), codeUnavailableCritical, "unsupported control"))}, false
	}
	if op.cls != 1 {
		return [][]byte{encodeMessage(id, encodeResult(1, codeProtocolError, "bad operation"))}, false
	}
	switch op.tag {
	case 0:
		return [][]byte{encodeMessage(id, doBind(d, st, op))}, false
	case 2:
		return nil, true
	case 3:
		return doSearch(d, st, id, op), false
	case 16:
		return nil, false
	case 6, 8, 10, 12, 14, 23:
		return [][]byte{encodeMessage(id, encodeResult(op.tag+1, codeUnwillingToPerform, "read only"))}, false
	default:
		return [][]byte{encodeMessage(id, encodeResult(1, codeProtocolError, "unsupported"))}, false
	}
}

func opResultTag(tag int) int {
	switch tag {
	case 0, 6, 8, 10, 12, 14, 23:
		return tag + 1
	default:
		return 1
	}
}

func opControlCritical(el ber) bool {
	if el.cls != 2 || el.tag != 0 {
		return false
	}
	for _, c := range el.kids {
		for _, f := range c.kids {
			if f.cls == 0 && f.tag == 1 && f.boolean() {
				return true
			}
		}
	}
	return false
}

func doBind(d *Directory, st *bindState, op ber) []byte {
	if len(op.kids) < 3 {
		return encodeResult(1, codeProtocolError, "bad bind")
	}
	ver, err := op.kids[0].integer()
	if err != nil || ver != 3 {
		return encodeResult(1, codeProtocolError, "LDAP version 3 required")
	}
	dn := strings.TrimSpace(op.kids[1].text())
	auth := op.kids[2]
	if auth.cls != 2 || auth.tag != 0 || auth.cons {
		return encodeResult(1, codeAuthMethodNotSupported, "simple bind only")
	}
	password := auth.text()
	st.ok = false
	st.anon = false
	if dn == "" && password == "" {
		st.ok = true
		st.anon = true
		return encodeResult(1, codeSuccess, "")
	}
	if password == "" {
		return encodeResult(1, codeInvalidCredentials, "")
	}
	if normalizeDN(dn) == normalizeDN(d.BindDN) && d.BindPassword != "" &&
		subtle.ConstantTimeCompare([]byte(password), []byte(d.BindPassword)) == 1 {
		st.ok = true
		return encodeResult(1, codeSuccess, "")
	}
	e := d.byDN[normalizeDN(dn)]
	if e == nil || e.Password == "" {
		return encodeResult(1, codeInvalidCredentials, "")
	}
	if bcrypt.CompareHashAndPassword([]byte(e.Password), []byte(password)) != nil {
		return encodeResult(1, codeInvalidCredentials, "")
	}
	st.ok = true
	return encodeResult(1, codeSuccess, "")
}

func doSearch(d *Directory, st *bindState, id int, op ber) [][]byte {
	fail := func(code int, msg string) [][]byte {
		return [][]byte{encodeMessage(id, encodeResult(5, code, msg))}
	}
	if !st.ok {
		return fail(codeInsufficientAccess, "bind required")
	}
	if len(op.kids) < 8 {
		return fail(codeProtocolError, "bad search")
	}
	base := op.kids[0].text()
	scope64, err := op.kids[1].integer()
	if err != nil {
		return fail(codeProtocolError, "bad scope")
	}
	scope := int(scope64)
	size64, err := op.kids[3].integer()
	if err != nil {
		return fail(codeProtocolError, "bad size")
	}
	typesOnly := op.kids[5].boolean()
	f, err := parseFilter(op.kids[6])
	if err != nil {
		return fail(codeProtocolError, "bad filter")
	}
	var attrs []string
	for _, a := range op.kids[7].kids {
		attrs = append(attrs, a.text())
	}
	if st.anon && !(strings.TrimSpace(base) == "" && scope == 0) {
		return fail(codeInsufficientAccess, "anonymous search is limited to the root DSE")
	}
	if scope < 0 || scope > 2 {
		return fail(codeProtocolError, "bad scope")
	}
	if !d.hasBase(base) {
		return fail(codeNoSuchObject, "base not found")
	}
	var out [][]byte
	limit := int(size64)
	n := 0
	truncated := false
	for _, e := range d.candidates(base, scope) {
		if !d.match(e, f) {
			continue
		}
		if limit > 0 && n >= limit {
			truncated = true
			break
		}
		n++
		out = append(out, encodeMessage(id, encodeEntry(e.DN, project(e, attrs, typesOnly))))
	}
	code := codeSuccess
	if truncated {
		code = codeSizeLimit
	}
	out = append(out, encodeMessage(id, encodeResult(5, code, "")))
	return out
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// StatePath is the on-disk directory snapshot. Empty uses the default.
// The file holds password hashes and the search-account password.
func StatePath(override string) string {
	if override != "" {
		return override
	}
	return "/var/lib/factum2/ldap/cache.json"
}

// Command is the worker.commands name that starts the LDAP listener.
const Command = "ldap"

// ValidateListen accepts an empty address or host:port.
func ValidateListen(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return errors.New("ldap listen must be host:port")
	}
	return nil
}

// Port returns the TCP port of a listen address. 0 if it cannot be read.
func Port(listen string) uint16 {
	_, p, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range p {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 || n > 65535 {
		return 0
	}
	return uint16(n)
}
