package drivers

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/util"
)

const (
	sessionUnixBaseURL = "http://factum2-driver"
	sessionMaxBody     = 32 << 20 // 32 MiB, same as hubMaxMessageSize
	sessionReadHeader  = 10 * time.Second
	sessionDialTimeout = 10 * time.Second
)

var (
	sessionTLSHandshakeTimeout = 10 * time.Second
	sessionProbeTimeout        = 3 * time.Second
)

type sessionHTTPClient struct {
	client *http.Client
	base   string
	token  string
	socket string
}

type rtFlagKey struct{}

type rtFlag struct {
	dialed bool
}

func markDialed(ctx context.Context) {
	if f, ok := ctx.Value(rtFlagKey{}).(*rtFlag); ok {
		f.dialed = true
	}
}

func newSessionUnixClient(socket string) *sessionHTTPClient {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: sessionDialTimeout}
			c, err := d.DialContext(ctx, "unix", socket)
			if err == nil {
				markDialed(ctx)
			}
			return c, err
		},
	}
	return &sessionHTTPClient{
		client: &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: tr,
		},
		base:   sessionUnixBaseURL,
		socket: socket,
	}
}

func newSessionURLClient(rawURL, token, caPath string) (*sessionHTTPClient, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("driver.tls_ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("driver.tls_ca: no certificates in %s", caPath)
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{
		Proxy:               nil,
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: sessionTLSHandshakeTimeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: sessionDialTimeout}
			c, err := d.DialContext(ctx, network, addr)
			if err == nil {
				markDialed(ctx)
			}
			return c, err
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: sessionDialTimeout}
			raw, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			cfg := tlsCfg.Clone()
			if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil {
				cfg.ServerName = host
			} else {
				cfg.ServerName = addr
			}
			conn := tls.Client(raw, cfg)
			hsCtx, cancel := context.WithTimeout(ctx, sessionTLSHandshakeTimeout)
			defer cancel()
			if err := conn.HandshakeContext(hsCtx); err != nil {
				raw.Close()
				return nil, err
			}
			markDialed(ctx)
			return conn, nil
		},
	}
	return &sessionHTTPClient{
		client: &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: tr,
		},
		base:  strings.TrimRight(rawURL, "/"),
		token: token,
	}, nil
}

type cliRunRequestJSON struct {
	Host      string          `json:"host"`
	Port      string          `json:"port"`
	Username  string          `json:"username"`
	Password  string          `json:"password"`
	Platform  string          `json:"platform"`
	Mode      string          `json:"mode"`
	Actor     string          `json:"actor"`
	TimeoutMS int             `json:"timeout_ms"`
	Cmds      []cliRunCmdJSON `json:"cmds"`
}

type cliRunCmdJSON struct {
	Cmd       string `json:"cmd"`
	EndMarker string `json:"end_marker"`
}

type cliRunResponseJSON struct {
	Outputs     []string `json:"outputs"`
	Reused      bool     `json:"reused"`
	LoginMS     int64    `json:"login_ms"`
	QueueWaitMS int64    `json:"queue_wait_ms"`
	CommandMS   int64    `json:"command_ms"`
}

type remoteStatusError struct {
	status int
	msg    string
}

func (e *remoteStatusError) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("ssh session remote: HTTP %d: %s", e.status, e.msg)
	}
	return fmt.Sprintf("ssh session remote: HTTP %d", e.status)
}

func endMarkerToken(re *regexp.Regexp) string {
	if re == nil {
		return ""
	}
	if re == vrpConfigEndMarker {
		return "vrp_config"
	}
	if re == iosxrConfigEndMarker {
		return "iosxr_config"
	}
	if re == srosConfigEndMarker {
		return "sros_config"
	}
	return ""
}

func endMarkerFromToken(tok string) (*regexp.Regexp, error) {
	switch tok {
	case "", "none":
		return nil, nil
	case "vrp_config":
		return vrpConfigEndMarker, nil
	case "iosxr_config":
		return iosxrConfigEndMarker, nil
	case "sros_config":
		return srosConfigEndMarker, nil
	default:
		return nil, fmt.Errorf("unknown end_marker %q", tok)
	}
}

func (c *sessionHTTPClient) Run(ctx context.Context, req sshRunRequest) (*sshRunResult, error) {
	if c.socket != "" {
		if _, err := os.Stat(c.socket); err != nil {
			return nil, fmt.Errorf("%w: %v", errRemoteConnect, err)
		}
	}
	mode := "batch"
	if req.Mode == sshRunPipeline {
		mode = "pipeline"
	}
	body := cliRunRequestJSON{
		Host:      req.Param.Name,
		Port:      req.Param.Port,
		Username:  req.Param.Username,
		Password:  req.Param.Password,
		Platform:  req.Param.Platform,
		Mode:      mode,
		Actor:     req.Actor,
		TimeoutMS: int(req.Timeout / time.Millisecond),
		Cmds:      make([]cliRunCmdJSON, len(req.Cmds)),
	}
	for i, cmd := range req.Cmds {
		body.Cmds[i] = cliRunCmdJSON{Cmd: cmd.Cmd, EndMarker: endMarkerToken(cmd.EndMarker)}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/cli/run", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	flag := &rtFlag{}
	httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), rtFlagKey{}, flag))

	resp, err := c.client.Do(httpReq)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, classifyClientErr(err, flag)
	}
	// HTTP status received: never a connect-failure.
	limited := io.LimitReader(resp.Body, sessionMaxBody+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(respBody) > sessionMaxBody {
		return nil, fmt.Errorf("ssh session remote: response too large")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseRemoteStatus(resp.StatusCode, respBody)
	}
	var out cliRunResponseJSON
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("ssh session remote: decode: %w", err)
	}
	return &sshRunResult{
		Outputs:   out.Outputs,
		Reused:    out.Reused,
		Login:     time.Duration(out.LoginMS) * time.Millisecond,
		QueueWait: time.Duration(out.QueueWaitMS) * time.Millisecond,
		Command:   time.Duration(out.CommandMS) * time.Millisecond,
	}, nil
}

func classifyClientErr(err error, flag *rtFlag) error {
	if err == nil {
		return nil
	}
	// dialed is set only after unix Dial or TLS handshake succeeds, i.e.
	// after the HTTP request may already have been written. Handshake and
	// certificate errors happen before that flag.
	if flag != nil && flag.dialed {
		return err
	}
	return fmt.Errorf("%w: %v", errRemoteConnect, err)
}

func (c *sessionHTTPClient) probeHealth() bool {
	ctx, cancel := context.WithTimeout(context.Background(), sessionProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return false
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return true
	}
	return err == nil
}

func parseRemoteStatus(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var wrapped struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &wrapped) == nil && wrapped.Error != "" {
		msg = wrapped.Error
	}
	return &remoteStatusError{status: status, msg: msg}
}

func writeSessionJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func bearerOK(header, want string) bool {
	if want == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	presented := strings.TrimPrefix(header, prefix)
	if presented == "" {
		return false
	}
	return passwordEqual(presented, want)
}

type sessionMux struct {
	token       string
	authHealth  bool
	cidrs       []*net.IPNet
	enforceCIDR bool
	lookup      func(ctx context.Context, host string) ([]net.IP, error)
}

func lookupHostIPs(ctx context.Context, host string) ([]net.IP, error) {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func (m *sessionMux) checkHost(ctx context.Context, host string) error {
	if !m.enforceCIDR {
		return nil
	}
	lookup := m.lookup
	if lookup == nil {
		lookup = lookupHostIPs
	}
	ips, err := lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("host not allowed")
	}
	for _, ip := range ips {
		allowed := false
		for _, n := range m.cidrs {
			if n.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("host not allowed")
		}
	}
	return nil
}

func (m *sessionMux) wrap(h http.HandlerFunc, isHealth bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.token != "" && (!isHealth || m.authHealth) {
			if !bearerOK(r.Header.Get("Authorization"), m.token) {
				writeSessionJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), defaultHTTPTimeout)
		defer cancel()
		h(w, r.WithContext(ctx))
	}
}

func (m *sessionMux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (m *sessionMux) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		stats := getPool().Stats()
		if stats == nil {
			stats = []sshSessionStat{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	case http.MethodDelete:
		key := r.URL.Query().Get("key")
		if key == "" {
			writeSessionJSONError(w, http.StatusBadRequest, "missing key")
			return
		}
		if !getPool().Evict(key) {
			writeSessionJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeSessionJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (m *sessionMux) handleCLIRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSessionJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sessionMaxBody)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeSessionJSONError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		writeSessionJSONError(w, http.StatusBadRequest, "read body")
		return
	}
	var body cliRunRequestJSON
	if err := json.Unmarshal(raw, &body); err != nil {
		writeSessionJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Host == "" || body.Username == "" || body.Platform == "" {
		writeSessionJSONError(w, http.StatusBadRequest, "host, username, and platform are required")
		return
	}
	mode := sshRunBatch
	switch body.Mode {
	case "", "batch":
		mode = sshRunBatch
	case "pipeline":
		mode = sshRunPipeline
	default:
		writeSessionJSONError(w, http.StatusBadRequest, "mode must be batch or pipeline")
		return
	}
	cmds := make([]sshCmd, len(body.Cmds))
	for i, c := range body.Cmds {
		re, err := endMarkerFromToken(c.EndMarker)
		if err != nil {
			writeSessionJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		cmds[i] = sshCmd{Cmd: c.Cmd, EndMarker: re}
	}
	if err := m.checkHost(r.Context(), body.Host); err != nil {
		writeSessionJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	timeout := time.Duration(body.TimeoutMS) * time.Millisecond
	req := sshRunRequest{
		Param: DriverParam{
			Name:     body.Host,
			Port:     body.Port,
			Username: body.Username,
			Password: body.Password,
			Platform: body.Platform,
		},
		Mode:    mode,
		Cmds:    cmds,
		Actor:   body.Actor,
		Timeout: timeout,
	}
	slog.Info("ssh.http_run", "host", body.Host, "platform", body.Platform, "actor", body.Actor)
	var res *sshRunResult
	if sshUseMemoryPool(body.Platform) {
		res, err = localRun(r.Context(), req)
	} else {
		res, err = oneShotRun(r.Context(), req)
	}
	if err != nil {
		status, msg := mapPoolHTTPError(err)
		writeSessionJSONError(w, status, msg)
		return
	}
	n := 0
	for _, o := range res.Outputs {
		n += len(o)
	}
	if n > sessionMaxBody {
		writeSessionJSONError(w, http.StatusBadGateway, "capture too large")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cliRunResponseJSON{
		Outputs:     res.Outputs,
		Reused:      res.Reused,
		LoginMS:     res.Login.Milliseconds(),
		QueueWaitMS: res.QueueWait.Milliseconds(),
		CommandMS:   res.Command.Milliseconds(),
	})
}

func oneShotRun(ctx context.Context, req sshRunRequest) (*sshRunResult, error) {
	s, err := dialSSHShell(req.Param.Username, req.Param.Password, req.Param.Name, req.Param.Port)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	_ = s.waitIdle(ctx, nil)
	start := time.Now()
	if req.Mode == sshRunPipeline {
		s.resetCapture()
		cmds := make([]string, len(req.Cmds))
		var marker *regexp.Regexp
		for i, c := range req.Cmds {
			cmds[i] = c.Cmd
			if c.EndMarker != nil {
				marker = c.EndMarker
			}
		}
		if err := s.write(strings.Join(cmds, "\n")); err != nil {
			return nil, err
		}
		_ = s.waitIdle(ctx, marker)
		out := strings.ReplaceAll(s.capture(), "\r\n", "\n")
		return &sshRunResult{Outputs: []string{out}, Command: time.Since(start)}, nil
	}
	results := make([]string, len(req.Cmds))
	for i, cmd := range req.Cmds {
		s.resetCapture()
		if err := s.write(cmd.Cmd); err != nil {
			return nil, err
		}
		_ = s.waitIdle(ctx, cmd.EndMarker)
		results[i] = s.capture()
	}
	return &sshRunResult{Outputs: normalizeSSHOutputs(results), Command: time.Since(start)}, nil
}

func mapPoolHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, errQueueFull), errors.Is(err, errPoolFull):
		return http.StatusTooManyRequests, err.Error()
	case errors.Is(err, errAcquireTimeout):
		return http.StatusRequestTimeout, err.Error()
	default:
		return http.StatusBadGateway, err.Error()
	}
}

func newSessionHandler(m *sessionMux) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.wrap(m.handleHealth, true))
	mux.HandleFunc("/v1/sessions", m.wrap(m.handleSessions, false))
	mux.HandleFunc("/v1/cli/run", m.wrap(m.handleCLIRun, false))
	return mux
}

type sessionListenPlan struct {
	unixPath    string
	tcpAddr     string
	token       string
	tlsCfg      *tls.Config
	nonLoopback bool
	cidrs       []*net.IPNet
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func parseAllowCIDRs(vals []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range vals {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("driver.allow_cidrs: invalid CIDR %q", s)
		}
		bits := 128
		if ip.To4() != nil {
			bits = 32
			ip = ip.To4()
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

func parseSessionListen(d util.ConfigDriver) (*sessionListenPlan, error) {
	unixPath := SessionSocketPath(d.Socket)
	tcpAddr := strings.TrimSpace(d.Listen)
	if tcpAddr == "" || tcpAddr == "none" {
		tcpAddr = ""
	}
	if unixPath == "" && tcpAddr == "" {
		return nil, fmt.Errorf("factum2-driver start: no listener (socket disabled and listen unset)")
	}
	plan := &sessionListenPlan{unixPath: unixPath, tcpAddr: tcpAddr}
	if tcpAddr == "" {
		return plan, nil
	}
	if d.Token == "" {
		return nil, fmt.Errorf("factum2-driver start: driver.token is required when driver.listen is set")
	}
	plan.token = d.Token
	if !isLoopbackListen(tcpAddr) {
		plan.nonLoopback = true
		if d.TLSCert == "" || d.TLSKey == "" {
			return nil, fmt.Errorf("factum2-driver start: tls_cert and tls_key are required for non-loopback listen")
		}
		cidrs, err := parseAllowCIDRs(d.AllowCIDRs)
		if err != nil {
			return nil, err
		}
		if len(cidrs) == 0 {
			return nil, fmt.Errorf("factum2-driver start: allow_cidrs is required for non-loopback listen")
		}
		plan.cidrs = cidrs
		cert, err := tls.LoadX509KeyPair(d.TLSCert, d.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("factum2-driver start: load tls cert: %w", err)
		}
		plan.tlsCfg = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
	}
	return plan, nil
}

func prepareSessionSocketDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir session socket dir: %w", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return fmt.Errorf("chmod session socket dir: %w", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat session socket dir: %w", err)
	}
	if st.Mode().Perm()&0o007 != 0 {
		return fmt.Errorf("session socket dir %s is world-accessible (mode %o)", dir, st.Mode().Perm())
	}
	return nil
}

func listenSessionUnix(socketPath string) (net.Listener, error) {
	dir := filepath.Dir(socketPath)
	if err := prepareSessionSocketDir(dir); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove leftover session socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen session unix: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod session socket: %w", err)
	}
	return ln, nil
}

// ServeSSHSession binds unix and/or TCP listeners and serves until ctx is done.
// Handlers call localRun only; InitSSHPool must already have client remote off.
func ServeSSHSession(ctx context.Context, d util.ConfigDriver) error {
	plan, err := parseSessionListen(d)
	if err != nil {
		return err
	}
	type serving struct {
		ln  net.Listener
		srv *http.Server
	}
	var svcs []serving

	add := func(ln net.Listener, mux http.Handler) {
		svcs = append(svcs, serving{
			ln: ln,
			srv: &http.Server{
				Handler:           mux,
				ReadHeaderTimeout: sessionReadHeader,
			},
		})
	}

	if plan.unixPath != "" {
		ln, err := listenSessionUnix(plan.unixPath)
		if err != nil {
			return err
		}
		add(ln, newSessionHandler(&sessionMux{}))
	}
	if plan.tcpAddr != "" {
		ln, err := net.Listen("tcp", plan.tcpAddr)
		if err != nil {
			for _, s := range svcs {
				s.ln.Close()
			}
			return fmt.Errorf("listen session tcp: %w", err)
		}
		if plan.tlsCfg != nil {
			ln = tls.NewListener(ln, plan.tlsCfg)
		}
		add(ln, newSessionHandler(&sessionMux{
			token:       plan.token,
			authHealth:  plan.nonLoopback,
			cidrs:       plan.cidrs,
			enforceCIDR: plan.nonLoopback,
		}))
	}

	errCh := make(chan error, len(svcs))
	for i := range svcs {
		s := svcs[i]
		go func() {
			slog.Info("ssh.session_listen", "addr", s.ln.Addr().String())
			errCh <- s.srv.Serve(s.ln)
		}()
	}

	select {
	case <-ctx.Done():
		for _, s := range svcs {
			shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = s.srv.Shutdown(shCtx)
			cancel()
		}
		return nil
	case err := <-errCh:
		for _, s := range svcs {
			shCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = s.srv.Shutdown(shCtx)
			cancel()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
