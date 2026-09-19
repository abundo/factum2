package drivers

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/util"

	"golang.org/x/crypto/ssh"
)

const (
	defaultQueueDepth     = 8
	defaultAcquireTimeout = 60 * time.Second
	defaultJobTimeout     = overallTimeout
	defaultMaxSessions    = 64
	defaultIdleTimeout    = 8 * time.Minute
	defaultKeepalive      = 30 * time.Second
	defaultHTTPTimeout    = 180 * time.Second
	sshCmdLogMax          = 80

	// DefaultSessionSocket is the unix path factum2-driver start binds and
	// callers probe. Independent of the worker hub socket.
	DefaultSessionSocket = "/run/factum2-driver/session.sock"
)

var remoteRetry = 5 * time.Second

var (
	errPoolFull        = errors.New("ssh session pool is full")
	errQueueFull       = errors.New("ssh session queue is full")
	errAcquireTimeout  = errors.New("ssh session acquire timeout")
	errPagerLeftover   = errors.New("ssh pager leftover")
	errSSHPoolClosed   = errors.New("ssh session pool is closed")
	errSSHPoolNotReady = errors.New("ssh session is not ready")
	errRemoteConnect   = errors.New("ssh session remote connect failed")
)

// Line-anchored pager prompt. Do not use a substring "More:" — that matches
// English in MOTD, comments, and interface descriptions.
var pagerMore = regexp.MustCompile(`(?im)^\s*-{0,4}\s*More\s*-{2,}\s*$`)

type sshRunMode int

const (
	sshRunBatch sshRunMode = iota
	sshRunPipeline
)

type sshRunRequest struct {
	Param   DriverParam
	Mode    sshRunMode
	Cmds    []sshCmd
	Actor   string
	Timeout time.Duration
}

type sshRunResult struct {
	Outputs   []string
	Reused    bool
	Login     time.Duration
	QueueWait time.Duration
	Command   time.Duration
}

type sshSessionStat struct {
	Key           string  `json:"key"`
	Platform      string  `json:"platform"`
	State         string  `json:"state"`
	AgeSeconds    float64 `json:"age_seconds"`
	IdleSeconds   float64 `json:"idle_seconds"`
	Reused        uint64  `json:"reuse_count"`
	Reconnects    uint64  `json:"reconnects"`
	LastLoginMs   int64   `json:"last_login_ms"`
	LastCommandMs int64   `json:"last_command_ms"`
	LastQueueMs   int64   `json:"last_queue_wait_ms"`
	LastError     string  `json:"last_error,omitempty"`
	LastActor     string  `json:"last_actor,omitempty"`
	LastCmd       string  `json:"last_cmd,omitempty"`
	LastHost      string  `json:"last_host,omitempty"`
}

// SSHPoolConfig is the process-lifetime pool knobs. Zero / empty values mean
// compiled defaults. Negative ints and durations are rejected by InitSSHPool.
type SSHPoolConfig struct {
	Platforms      *[]string
	IdleTimeout    time.Duration
	MaxSessions    int
	QueueDepth     int
	AcquireTimeout time.Duration
	Keepalive      time.Duration
	SessionURL     string
	SessionToken   string
	TLSCA          string
	Socket         string
}

type hygieneProfile struct {
	requiredSetup   []string
	bestEffortSetup []string
	configEntry     string
	exitCmds        []string
	resetCmd        string
}

func (p *hygieneProfile) setupCmds() []string {
	out := make([]string, 0, len(p.requiredSetup)+len(p.bestEffortSetup))
	out = append(out, p.requiredSetup...)
	out = append(out, p.bestEffortSetup...)
	return out
}

var hygieneProfiles = map[string]*hygieneProfile{
	"vrp": {
		requiredSetup: []string{"screen-length 0 temporary"},
		configEntry:   "system-view",
		exitCmds:      []string{"return", "quit"},
		resetCmd:      "return",
	},
	"ciscosmb": {
		requiredSetup:   []string{"terminal datadump"},
		bestEffortSetup: []string{"terminal width 0"},
		configEntry:     "configure",
		exitCmds:        []string{"end"},
		resetCmd:        "end",
	},
}

type sshPoolGlobals struct {
	mu           sync.Mutex
	inited       bool
	enabled      map[string]struct{}
	legacyLogged map[string]struct{}
	pool         *memoryPool
	poolCfg      SSHPoolConfig
	remote       *sessionRemote
}

type sessionRemote struct {
	client    *sessionHTTPClient
	socket    string
	baseURL   string
	down      bool
	lastProbe time.Time
}

var sshGlobals sshPoolGlobals

// SessionSocketPath resolves the session-owner unix socket for both
// factum2-driver start (listen) and callers (probe). yamlOverride is
// driver.socket. FACTUM_DRIVER_SESSION_SOCKET relocates or disables when
// yaml is empty. "none" and "0" disable unix. Must not use HubSocketPath.
func SessionSocketPath(yamlOverride string) string {
	if yamlOverride == "none" || yamlOverride == "0" {
		return ""
	}
	if yamlOverride != "" {
		return yamlOverride
	}
	switch v := os.Getenv("FACTUM_DRIVER_SESSION_SOCKET"); v {
	case "none", "0":
		return ""
	case "":
		return DefaultSessionSocket
	default:
		return v
	}
}

func sessionKey(p DriverParam) string {
	_, _, addr := sshDialHostPort(p)
	return canonicalPlatform(p.Platform) + "/" + p.Username + "@" + addr
}

func canonicalPlatform(platform string) string {
	platform = strings.ToLower(platform)
	if platform == "sros-md" {
		return "sros"
	}
	return platform
}

func sshDialHostPort(p DriverParam) (host, port, addr string) {
	host = p.Name
	port = p.Port
	if port == "" {
		port = sshDefaultPort
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host, port, net.JoinHostPort(host, port)
}

func enabledPlatformSet(platforms *[]string) map[string]struct{} {
	if platforms == nil {
		return map[string]struct{}{"vrp": {}, "ciscosmb": {}}
	}
	out := map[string]struct{}{}
	for _, p := range *platforms {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "none" {
			return map[string]struct{}{}
		}
		if p == "" {
			continue
		}
		out[p] = struct{}{}
	}
	return out
}

func applySSHPoolDefaults(cfg SSHPoolConfig) (SSHPoolConfig, error) {
	if cfg.MaxSessions < 0 {
		return cfg, fmt.Errorf("driver.max_sessions must not be negative")
	}
	if cfg.QueueDepth < 0 {
		return cfg, fmt.Errorf("driver.queue_depth must not be negative")
	}
	if cfg.IdleTimeout < 0 {
		return cfg, fmt.Errorf("driver.idle_timeout must not be negative")
	}
	if cfg.AcquireTimeout < 0 {
		return cfg, fmt.Errorf("driver.acquire_timeout must not be negative")
	}
	if cfg.Keepalive < 0 {
		return cfg, fmt.Errorf("driver.keepalive must not be negative")
	}
	if cfg.MaxSessions == 0 {
		cfg.MaxSessions = defaultMaxSessions
	}
	if cfg.QueueDepth == 0 {
		cfg.QueueDepth = defaultQueueDepth
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.AcquireTimeout == 0 {
		cfg.AcquireTimeout = defaultAcquireTimeout
	}
	if cfg.Keepalive == 0 {
		cfg.Keepalive = defaultKeepalive
	}
	return cfg, nil
}

// PoolConfigFromDriver maps YAML ConfigDriver onto SSHPoolConfig.
// Empty duration strings stay zero (compiled defaults at InitSSHPool).
func PoolConfigFromDriver(d util.ConfigDriver) (SSHPoolConfig, error) {
	cfg := SSHPoolConfig{
		Platforms:    d.Platforms,
		MaxSessions:  d.MaxSessions,
		QueueDepth:   d.QueueDepth,
		SessionURL:   d.SessionURL,
		SessionToken: d.SessionToken,
		TLSCA:        d.TLSCA,
		Socket:       d.Socket,
	}
	var err error
	if d.IdleTimeout != "" {
		cfg.IdleTimeout, err = time.ParseDuration(d.IdleTimeout)
		if err != nil {
			return cfg, fmt.Errorf("driver.idle_timeout: %w", err)
		}
	}
	if d.AcquireTimeout != "" {
		cfg.AcquireTimeout, err = time.ParseDuration(d.AcquireTimeout)
		if err != nil {
			return cfg, fmt.Errorf("driver.acquire_timeout: %w", err)
		}
	}
	if d.Keepalive != "" {
		cfg.Keepalive, err = time.ParseDuration(d.Keepalive)
		if err != nil {
			return cfg, fmt.Errorf("driver.keepalive: %w", err)
		}
	}
	return cfg, nil
}

// InitSSHPoolFromDriver compiles YAML knobs and calls InitSSHPool.
func InitSSHPoolFromDriver(d util.ConfigDriver) error {
	cfg, err := PoolConfigFromDriver(d)
	if err != nil {
		return err
	}
	return InitSSHPool(cfg)
}

// InitSSHPool installs the process-lifetime in-process SSH CLI pool.
// It returns an error on invalid knobs and panics on double-init.
func InitSSHPool(cfg SSHPoolConfig) error {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if sshGlobals.inited {
		panic("drivers: InitSSHPool called twice")
	}
	cfg, err := applySSHPoolDefaults(cfg)
	if err != nil {
		return err
	}
	sshGlobals.enabled = enabledPlatformSet(cfg.Platforms)
	sshGlobals.legacyLogged = map[string]struct{}{}
	sshGlobals.poolCfg = cfg
	sshGlobals.pool = newMemoryPool(cfg)
	if err := setupRemoteLocked(cfg); err != nil {
		sshGlobals.pool.Close()
		sshGlobals.pool = nil
		sshGlobals.enabled = nil
		return err
	}
	sshGlobals.inited = true
	return nil
}

func setupRemoteLocked(cfg SSHPoolConfig) error {
	if cfg.SessionURL != "" {
		client, err := newSessionURLClient(cfg.SessionURL, cfg.SessionToken, cfg.TLSCA)
		if err != nil {
			return err
		}
		sshGlobals.remote = &sessionRemote{client: client, baseURL: cfg.SessionURL}
		slog.Info("ssh.remote", "state", "configured", "url", cfg.SessionURL)
		return nil
	}
	path := SessionSocketPath(cfg.Socket)
	if path == "" {
		sshGlobals.remote = nil
		return nil
	}
	client := newSessionUnixClient(path)
	r := &sessionRemote{client: client, socket: path, baseURL: sessionUnixBaseURL}
	if _, err := os.Stat(path); err != nil {
		r.down = true
		r.lastProbe = time.Now()
		slog.Info("ssh.remote", "state", "down", "socket", path)
	} else {
		slog.Info("ssh.remote", "state", "up", "socket", path)
	}
	sshGlobals.remote = r
	return nil
}

func stopSSHPoolLocked() {
	if sshGlobals.pool != nil {
		sshGlobals.pool.Close()
		sshGlobals.pool = nil
	}
	sshGlobals.remote = nil
	sshGlobals.enabled = nil
	sshGlobals.inited = false
}

func replaceMemoryPoolLocked() {
	if sshGlobals.pool != nil {
		sshGlobals.pool.Close()
	}
	sshGlobals.pool = newMemoryPool(sshGlobals.poolCfg)
}

// CloseSSHPool closes every in-process SSH client. Used by factum2-driver start on SIGTERM.
func CloseSSHPool() {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if sshGlobals.pool != nil {
		sshGlobals.pool.Close()
	}
}

// ResetSSHPoolForTest closes the process pool and re-inits compiled defaults
// with remote probe off so tests stay in-process.
func ResetSSHPoolForTest() {
	sshGlobals.mu.Lock()
	stopSSHPoolLocked()
	sshGlobals.mu.Unlock()
	if err := InitSSHPool(SSHPoolConfig{Socket: "none"}); err != nil {
		panic(err)
	}
}

func initSSHPoolForTest(cfg SSHPoolConfig) error {
	sshGlobals.mu.Lock()
	stopSSHPoolLocked()
	sshGlobals.mu.Unlock()
	if cfg.Socket == "" && cfg.SessionURL == "" {
		cfg.Socket = "none"
	}
	return InitSSHPool(cfg)
}

func clearSSHPoolForTest() {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	stopSSHPoolLocked()
}

func getPool() *memoryPool {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if !sshGlobals.inited {
		panic("drivers: InitSSHPool was not called; call initDriverSSHPool / web.GUI")
	}
	return sshGlobals.pool
}

func runPooled(ctx context.Context, req sshRunRequest) (*sshRunResult, error) {
	if client, ok := takeRemote(); ok {
		res, err := client.Run(ctx, req)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, errRemoteConnect) {
			return nil, err
		}
		markRemoteDown()
		slog.Warn("ssh.remote_fallback", "key", sessionKey(req.Param), "err", err)
		return getPool().Run(ctx, req)
	}
	return getPool().Run(ctx, req)
}

func takeRemote() (*sessionHTTPClient, bool) {
	sshGlobals.mu.Lock()
	if !sshGlobals.inited {
		sshGlobals.mu.Unlock()
		panic("drivers: InitSSHPool was not called; call initDriverSSHPool / web.GUI")
	}
	r := sshGlobals.remote
	if r == nil {
		sshGlobals.mu.Unlock()
		return nil, false
	}
	if !r.down {
		c := r.client
		sshGlobals.mu.Unlock()
		return c, true
	}
	now := time.Now()
	if now.Sub(r.lastProbe) < remoteRetry {
		sshGlobals.mu.Unlock()
		return nil, false
	}
	r.lastProbe = now
	sshGlobals.mu.Unlock()

	if r.client == nil || !r.client.probeHealth() {
		return nil, false
	}

	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if sshGlobals.remote != r {
		return nil, false
	}
	if r.down {
		r.down = false
		slog.Warn("ssh.remote_up", "socket", r.socket, "url", r.baseURL)
		replaceMemoryPoolLocked()
	}
	return r.client, true
}

func markRemoteDown() {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if sshGlobals.remote == nil {
		return
	}
	if !sshGlobals.remote.down {
		sshGlobals.remote.down = true
		sshGlobals.remote.lastProbe = time.Now()
	}
}

func sshUseMemoryPool(platform string) bool {
	sshGlobals.mu.Lock()
	defer sshGlobals.mu.Unlock()
	if !sshGlobals.inited {
		panic("drivers: InitSSHPool was not called; call initDriverSSHPool / web.GUI")
	}
	plat := strings.ToLower(platform)
	if _, ok := sshGlobals.enabled[plat]; !ok {
		logSSHLegacyLocked(platform, "not_enabled")
		return false
	}
	canon := canonicalPlatform(plat)
	if _, ok := hygieneProfiles[canon]; !ok {
		logSSHLegacyLocked(platform, "no_profile")
		return false
	}
	return true
}

func logSSHLegacyLocked(platform, reason string) {
	if sshGlobals.legacyLogged == nil {
		sshGlobals.legacyLogged = map[string]struct{}{}
	}
	key := platform + ":" + reason
	if _, ok := sshGlobals.legacyLogged[key]; ok {
		return
	}
	sshGlobals.legacyLogged[key] = struct{}{}
	slog.Info("ssh.legacy", "reason", reason, "platform", platform)
}

func truncateCmd(s string) string {
	if len(s) <= sshCmdLogMax {
		return s
	}
	return s[:sshCmdLogMax]
}

func passwordEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func hasPagerLeftover(output string) bool {
	return pagerMore.MatchString(output)
}

func requiredSetupError(platform, output string) string {
	switch canonicalPlatform(platform) {
	case "vrp":
		return vrpFindCLIError(output)
	case "ciscosmb":
		return smbFindCLIError(output)
	}
	return ""
}

func elidePreamble(cmds []sshCmd, setup []string) []sshCmd {
	if len(setup) == 0 || len(cmds) < len(setup) {
		return cmds
	}
	for i, s := range setup {
		if cmds[i].Cmd != s {
			return cmds
		}
	}
	return cmds[len(setup):]
}

func needsReset(cmds []sshCmd, profile *hygieneProfile) bool {
	if profile == nil || profile.resetCmd == "" {
		return false
	}
	entered := false
	last := ""
	for _, c := range cmds {
		t := strings.TrimSpace(c.Cmd)
		if t == "" {
			continue
		}
		last = t
		if t == profile.configEntry {
			entered = true
		}
	}
	if !entered {
		return false
	}
	for _, ex := range profile.exitCmds {
		if last == ex {
			return false
		}
	}
	return true
}

type sessionState int

const (
	stateIdle sessionState = iota
	stateBusy
	stateDialing
	stateDead
)

func (s sessionState) String() string {
	switch s {
	case stateIdle:
		return "idle"
	case stateBusy:
		return "busy"
	case stateDialing:
		return "dialing"
	case stateDead:
		return "dead"
	default:
		return "absent"
	}
}

type acquireWaiter struct {
	ch chan struct{}
}

type pooledSession struct {
	pool     *memoryPool
	key      string
	platform string
	profile  *hygieneProfile

	mu       sync.Mutex
	busy     bool
	waiters  []acquireWaiter
	state    sessionState
	ready    bool
	shell    *sshShellSession
	password string
	kaStop   chan struct{}
	kaOff    bool
	created  time.Time
	lastUsed time.Time

	reuseCount  uint64
	reconnects  uint64
	lastLogin   time.Duration
	lastCommand time.Duration
	lastQueue   time.Duration
	lastError   string
	lastActor   string
	lastCmd     string
	lastHost    string
}

type memoryPool struct {
	mu             sync.Mutex
	sessions       map[string]*pooledSession
	maxSessions    int
	queueDepth     int
	acquireTimeout time.Duration
	idleTimeout    time.Duration
	keepalive      time.Duration
	closed         bool
	stopIdle       chan struct{}
}

func newMemoryPool(cfg SSHPoolConfig) *memoryPool {
	p := &memoryPool{
		sessions:       make(map[string]*pooledSession),
		maxSessions:    cfg.MaxSessions,
		queueDepth:     cfg.QueueDepth,
		acquireTimeout: cfg.AcquireTimeout,
		idleTimeout:    cfg.IdleTimeout,
		keepalive:      cfg.Keepalive,
		stopIdle:       make(chan struct{}),
	}
	go p.idleLoop()
	return p
}

func (p *memoryPool) idleLoop() {
	tick := p.idleTimeout / 4
	if tick > time.Second {
		tick = time.Second
	}
	if tick < 20*time.Millisecond {
		tick = 20 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-p.stopIdle:
			return
		case <-t.C:
			p.evictIdle()
		}
	}
}

func (p *memoryPool) evictIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	now := time.Now()
	for key, s := range p.sessions {
		s.mu.Lock()
		idle := !s.busy && len(s.waiters) == 0 && s.state == stateIdle
		tooOld := !s.lastUsed.IsZero() && now.Sub(s.lastUsed) >= p.idleTimeout
		if idle && tooOld {
			s.closeLocked("idle_timeout")
			s.mu.Unlock()
			delete(p.sessions, key)
			slog.Info("ssh.evict", "key", key, "reason", "idle_timeout")
			continue
		}
		s.mu.Unlock()
	}
}

func (p *memoryPool) evictLRUIdleLocked() bool {
	var oldest *pooledSession
	var oldestKey string
	var oldestUsed time.Time
	for key, s := range p.sessions {
		s.mu.Lock()
		idle := !s.busy && len(s.waiters) == 0 && (s.state == stateIdle || s.state == stateDead)
		used := s.lastUsed
		s.mu.Unlock()
		if !idle {
			continue
		}
		if oldest == nil || used.Before(oldestUsed) {
			oldest = s
			oldestKey = key
			oldestUsed = used
		}
	}
	if oldest == nil {
		return false
	}
	oldest.mu.Lock()
	if oldest.busy || len(oldest.waiters) > 0 {
		oldest.mu.Unlock()
		return false
	}
	oldest.closeLocked("max_sessions")
	oldest.mu.Unlock()
	delete(p.sessions, oldestKey)
	slog.Info("ssh.evict", "key", oldestKey, "reason", "max_sessions")
	return true
}

// holdSession looks up or creates the session and marks it busy before
// returning, so idle/LRU eviction cannot close a session another Run holds.
func (p *memoryPool) holdSession(ctx context.Context, key, platform string) (*pooledSession, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errSSHPoolClosed
	}
	s, ok := p.sessions[key]
	if !ok {
		if len(p.sessions) >= p.maxSessions {
			if !p.evictLRUIdleLocked() {
				p.mu.Unlock()
				return nil, errPoolFull
			}
		}
		s = &pooledSession{
			pool:     p,
			key:      key,
			platform: canonicalPlatform(platform),
			profile:  hygieneProfiles[canonicalPlatform(platform)],
			state:    stateBusy,
			created:  time.Now(),
			busy:     true,
		}
		p.sessions[key] = s
		p.mu.Unlock()
		return s, nil
	}
	s.mu.Lock()
	if !s.busy && len(s.waiters) == 0 {
		s.busy = true
		s.state = stateBusy
		s.mu.Unlock()
		p.mu.Unlock()
		return s, nil
	}
	if len(s.waiters) >= p.queueDepth {
		s.mu.Unlock()
		p.mu.Unlock()
		return nil, errQueueFull
	}
	w := acquireWaiter{ch: make(chan struct{})}
	s.waiters = append(s.waiters, w)
	s.mu.Unlock()
	p.mu.Unlock()

	timer := time.NewTimer(p.acquireTimeout)
	defer timer.Stop()
	select {
	case <-w.ch:
		return s, nil
	case <-ctx.Done():
		if s.removeWaiter(w.ch) {
			return nil, errAcquireTimeout
		}
		<-w.ch
		return s, nil
	case <-timer.C:
		if s.removeWaiter(w.ch) {
			return nil, errAcquireTimeout
		}
		<-w.ch
		return s, nil
	}
}

func (p *memoryPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.stopIdle)
	for k, s := range p.sessions {
		s.mu.Lock()
		s.closeLocked("pool_close")
		s.mu.Unlock()
		delete(p.sessions, k)
	}
}

func (p *memoryPool) Evict(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	s, ok := p.sessions[key]
	if !ok {
		return false
	}
	s.mu.Lock()
	s.closeLocked("evict")
	waiters := s.waiters
	s.waiters = nil
	s.busy = false
	s.mu.Unlock()
	delete(p.sessions, key)
	for _, w := range waiters {
		close(w.ch)
	}
	slog.Info("ssh.evict", "key", key, "reason", "api")
	return true
}

func (p *memoryPool) Stats() []sshSessionStat {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]sshSessionStat, 0, len(p.sessions))
	now := time.Now()
	for _, s := range p.sessions {
		s.mu.Lock()
		st := sshSessionStat{
			Key:           s.key,
			Platform:      s.platform,
			State:         s.state.String(),
			AgeSeconds:    now.Sub(s.created).Seconds(),
			Reused:        s.reuseCount,
			Reconnects:    s.reconnects,
			LastLoginMs:   s.lastLogin.Milliseconds(),
			LastCommandMs: s.lastCommand.Milliseconds(),
			LastQueueMs:   s.lastQueue.Milliseconds(),
			LastError:     s.lastError,
			LastActor:     s.lastActor,
			LastCmd:       s.lastCmd,
			LastHost:      s.lastHost,
		}
		if s.state == stateIdle && !s.lastUsed.IsZero() {
			st.IdleSeconds = now.Sub(s.lastUsed).Seconds()
		}
		s.mu.Unlock()
		out = append(out, st)
	}
	return out
}

func (s *pooledSession) removeWaiter(ch chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.waiters {
		if w.ch == ch {
			s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
			return true
		}
	}
	return false
}

func (s *pooledSession) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.waiters) > 0 {
		w := s.waiters[0]
		s.waiters = s.waiters[1:]
		s.busy = true
		if s.state != stateDead && s.state != stateDialing {
			s.state = stateBusy
		}
		close(w.ch)
		return
	}
	s.busy = false
	s.lastUsed = time.Now()
	if s.shell != nil && s.ready && s.state != stateDead {
		s.state = stateIdle
	}
}

func (s *pooledSession) closeLocked(reason string) {
	s.stopKeepaliveLocked()
	if s.shell != nil {
		s.shell.Close()
		s.shell = nil
	}
	s.ready = false
	s.password = ""
	s.state = stateDead
	_ = reason
}

func (s *pooledSession) markDead(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(reason)
}

func (s *pooledSession) stopKeepaliveLocked() {
	if s.kaStop != nil {
		close(s.kaStop)
		s.kaStop = nil
	}
}

func (s *pooledSession) startKeepalive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopKeepaliveLocked()
	s.kaOff = false
	stop := make(chan struct{})
	s.kaStop = stop
	go s.keepaliveLoop(stop)
}

func (s *pooledSession) keepaliveLoop(stop chan struct{}) {
	t := time.NewTicker(s.pool.keepalive)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.mu.Lock()
			shell := s.shell
			idle := s.state == stateIdle && !s.busy
			off := s.kaOff
			s.mu.Unlock()
			if off || shell == nil || shell.client == nil || !idle {
				continue
			}
			ok, _, err := shell.client.SendRequest("keepalive@openssh.com", true, nil)
			select {
			case <-stop:
				return
			default:
			}
			s.mu.Lock()
			stillMine := s.shell == shell && s.kaStop == stop
			s.mu.Unlock()
			if !stillMine {
				return
			}
			if err != nil {
				slog.Info("ssh.reconnect", "key", s.key, "reason", "keepalive")
				s.markDead("keepalive")
				return
			}
			if !ok {
				slog.Info("ssh.keepalive_unimplemented", "key", s.key)
				s.mu.Lock()
				if s.shell == shell && s.kaStop == stop {
					s.kaOff = true
				}
				s.mu.Unlock()
				return
			}
		}
	}
}

func dialSSHShellKeepalive(username, password, addr string, keepalive time.Duration) (*sshShellSession, error) {
	if keepalive <= 0 {
		keepalive = defaultKeepalive
	}
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: keepalive}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshClientConfig(username, password))
	if err != nil {
		conn.Close()
		return nil, err
	}
	client := ssh.NewClient(ncc, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := session.RequestPty("vt100", 50, 200, modes); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	return newSSHShellSession(client, session, stdin, stdout), nil
}

func (s *pooledSession) ensureDialed(ctx context.Context, req sshRunRequest) (login time.Duration, err error) {
	s.mu.Lock()
	live := s.shell != nil && s.ready && s.state != stateDead && !s.shell.stdoutExited()
	s.mu.Unlock()
	if live {
		return 0, nil
	}
	start := time.Now()
	s.mu.Lock()
	s.state = stateDialing
	if s.shell != nil {
		s.closeLocked("redial")
	}
	s.password = req.Param.Password
	s.mu.Unlock()

	_, _, addr := sshDialHostPort(req.Param)
	shell, err := dialSSHShellKeepalive(req.Param.Username, req.Param.Password, addr, s.pool.keepalive)
	if err != nil {
		s.markDead("dial")
		slog.Info("ssh.dial", "key", s.key, "login_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return 0, err
	}

	if err := shell.waitIdle(ctx, nil); err != nil {
		shell.Close()
		s.markDead("banner")
		slog.Info("ssh.dial", "key", s.key, "login_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return 0, err
	}
	if hasPagerLeftover(shell.capture()) {
		shell.Close()
		s.markDead("pager")
		slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
		return 0, errPagerLeftover
	}

	if err := s.runSetup(ctx, shell); err != nil {
		shell.Close()
		s.markDead("setup")
		slog.Info("ssh.dial", "key", s.key, "login_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return 0, err
	}

	login = time.Since(start)
	s.mu.Lock()
	s.shell = shell
	s.ready = true
	s.state = stateBusy
	s.lastLogin = login
	s.mu.Unlock()
	s.startKeepalive()
	slog.Info("ssh.dial", "key", s.key, "login_ms", login.Milliseconds(), "err", "")
	return login, nil
}

func (s *pooledSession) runSetup(ctx context.Context, shell *sshShellSession) error {
	if s.profile == nil {
		return nil
	}
	for _, cmd := range s.profile.requiredSetup {
		shell.resetCapture()
		if err := shell.write(cmd); err != nil {
			return err
		}
		if err := shell.waitIdle(ctx, nil); err != nil {
			return err
		}
		out := shell.capture()
		if hasPagerLeftover(out) {
			slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
			return errPagerLeftover
		}
		if line := requiredSetupError(s.platform, out); line != "" {
			return fmt.Errorf("ssh session setup: %s", line)
		}
	}
	for _, cmd := range s.profile.bestEffortSetup {
		shell.resetCapture()
		if err := shell.write(cmd); err != nil {
			return err
		}
		if err := shell.waitIdle(ctx, nil); err != nil {
			return err
		}
		out := shell.capture()
		if hasPagerLeftover(out) {
			slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
			return errPagerLeftover
		}
	}
	return nil
}

func (p *memoryPool) Run(ctx context.Context, req sshRunRequest) (*sshRunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Actor == "" {
		req.Actor = req.Param.actor
	}
	key := sessionKey(req.Param)
	host, _, _ := sshDialHostPort(req.Param)
	firstCmd := ""
	if len(req.Cmds) > 0 {
		firstCmd = req.Cmds[0].Cmd
	}

	acquireStart := time.Now()
	sess, err := p.holdSession(ctx, key, req.Param.Platform)
	if err != nil {
		return nil, err
	}
	defer sess.release()
	queueWait := time.Since(acquireStart)

	sess.mu.Lock()
	sess.lastActor = req.Actor
	sess.lastHost = host
	sess.lastCmd = truncateCmd(firstCmd)
	sess.lastQueue = queueWait
	password := sess.password
	hadLive := sess.shell != nil && sess.ready && sess.state != stateDead
	sess.mu.Unlock()

	if password != "" && !passwordEqual(password, req.Param.Password) {
		sess.markDead("password_change")
		hadLive = false
	}

	wasIdle := hadLive
	replayed := false
	var login time.Duration
	var result *sshRunResult
	var runErr error

	for {
		sess.mu.Lock()
		connDead := sess.shell == nil || sess.shell.stdoutExited() || sess.state == stateDead
		sess.mu.Unlock()
		if wasIdle && connDead && !replayed {
			slog.Info("ssh.replay", "key", key, "reason", "stale_idle")
			sess.markDead("stale_idle")
			wasIdle = false
			replayed = true
			sess.mu.Lock()
			sess.reconnects++
			sess.mu.Unlock()
		}

		login, runErr = sess.ensureDialed(ctx, req)
		if runErr != nil {
			sess.mu.Lock()
			sess.lastError = runErr.Error()
			sess.mu.Unlock()
			return nil, runErr
		}

		reused := login == 0 && !replayed
		cmdStart := time.Now()
		result, runErr = sess.runJob(ctx, req, wasIdle && !replayed)
		command := time.Since(cmdStart)

		if runErr != nil && shouldStaleReplay(wasIdle, replayed, runErr, result) {
			reason := staleReplayReason(runErr)
			slog.Info("ssh.replay", "key", key, "reason", reason)
			sess.markDead(reason)
			wasIdle = false
			replayed = true
			sess.mu.Lock()
			sess.reconnects++
			sess.mu.Unlock()
			continue
		}

		if result == nil {
			result = &sshRunResult{}
		}
		result.Reused = reused
		result.Login = login
		result.QueueWait = queueWait
		result.Command = command

		sess.mu.Lock()
		sess.lastCommand = command
		sess.lastLogin = login
		if reused {
			sess.reuseCount++
		}
		if runErr != nil {
			sess.lastError = runErr.Error()
		} else {
			sess.lastError = ""
		}
		sess.mu.Unlock()

		logCmd := firstCmd
		if sess.profile != nil {
			elided := elidePreamble(req.Cmds, sess.profile.setupCmds())
			if len(elided) > 0 {
				logCmd = elided[0].Cmd
			}
		}
		slog.Info("ssh.run",
			"key", key,
			"host", host,
			"platform", sess.platform,
			"reused", reused,
			"replayed", replayed,
			"login_ms", login.Milliseconds(),
			"queue_wait_ms", queueWait.Milliseconds(),
			"command_ms", command.Milliseconds(),
			"cmds", len(req.Cmds),
			"actor", req.Actor,
			"cmd", truncateCmd(logCmd),
		)
		return result, runErr
	}
}

func shouldStaleReplay(wasIdle, replayed bool, err error, result *sshRunResult) bool {
	if !wasIdle || replayed || err == nil {
		return false
	}
	if errors.Is(err, errStaleWrite) {
		return true
	}
	empty := result == nil || captureEmpty(result.Outputs)
	if empty && (errors.Is(err, io.EOF) || errors.Is(err, errWaitTimeout)) {
		return true
	}
	return false
}

func staleReplayReason(err error) string {
	if errors.Is(err, errStaleWrite) {
		return "write_fail"
	}
	if errors.Is(err, io.EOF) {
		return "empty_eof"
	}
	return "stale_idle"
}

func captureEmpty(outputs []string) bool {
	for _, o := range outputs {
		if strings.TrimSpace(o) != "" {
			return false
		}
	}
	return true
}

var errStaleWrite = errors.New("ssh stdin write failed")

func (s *pooledSession) runJob(ctx context.Context, req sshRunRequest, allowStale bool) (*sshRunResult, error) {
	s.mu.Lock()
	shell := s.shell
	ready := s.ready
	profile := s.profile
	s.mu.Unlock()
	if shell == nil || !ready {
		return nil, errSSHPoolNotReady
	}

	cmds := req.Cmds
	norig := len(cmds)
	if ready && profile != nil {
		cmds = elidePreamble(cmds, profile.setupCmds())
	}

	jobTimeout := req.Timeout
	if jobTimeout <= 0 {
		jobTimeout = defaultJobTimeout
	}
	jobCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	if allowStale && shell.stdoutExited() {
		return &sshRunResult{Outputs: make([]string, norig)}, fmt.Errorf("%w", io.EOF)
	}

	var outputs []string
	var err error
	switch req.Mode {
	case sshRunPipeline:
		outputs, err = s.runPipeline(jobCtx, shell, cmds, norig, allowStale)
	default:
		outputs, err = s.runBatch(jobCtx, shell, cmds, norig, allowStale)
	}
	if err != nil {
		if errors.Is(err, errStaleWrite) || errors.Is(err, io.EOF) || errors.Is(err, errWaitTimeout) || errors.Is(err, errPagerLeftover) {
			s.markDead(err.Error())
		} else {
			s.markDead("io")
		}
		return &sshRunResult{Outputs: outputs}, err
	}

	if needsReset(cmds, profile) {
		shell.resetCapture()
		if werr := shell.write(profile.resetCmd); werr != nil {
			s.markDead("reset")
			return &sshRunResult{Outputs: outputs}, werr
		}
		if werr := shell.waitIdle(jobCtx, nil); werr != nil {
			s.markDead("reset")
			return &sshRunResult{Outputs: outputs}, werr
		}
		out := shell.capture()
		if hasPagerLeftover(out) {
			slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
			s.markDead("pager")
			return &sshRunResult{Outputs: outputs}, errPagerLeftover
		}
	}

	if shell.stdoutExited() {
		s.markDead("eof")
		return &sshRunResult{Outputs: outputs}, io.EOF
	}
	return &sshRunResult{Outputs: outputs}, nil
}

func padOutputs(prefix int, ran []string, total int) []string {
	out := make([]string, total)
	copy(out[prefix:], ran)
	return out
}

func (s *pooledSession) runBatch(ctx context.Context, shell *sshShellSession, cmds []sshCmd, norig int, allowStale bool) ([]string, error) {
	prefix := norig - len(cmds)
	if prefix < 0 {
		prefix = 0
	}
	ran := make([]string, 0, len(cmds))
	for i, cmd := range cmds {
		shell.resetCapture()
		if err := shell.write(cmd.Cmd); err != nil {
			if allowStale && i == 0 {
				return padOutputs(prefix, ran, norig), fmt.Errorf("%w: %v", errStaleWrite, err)
			}
			return padOutputs(prefix, ran, norig), err
		}
		werr := shell.waitIdle(ctx, cmd.EndMarker)
		out := shell.capture()
		if hasPagerLeftover(out) {
			slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
			ran = append(ran, out)
			return padOutputs(prefix, ran, norig), errPagerLeftover
		}
		if werr != nil {
			if allowStale && i == 0 && strings.TrimSpace(out) == "" && (errors.Is(werr, io.EOF) || errors.Is(werr, errWaitTimeout)) {
				ran = append(ran, out)
				return padOutputs(prefix, ran, norig), werr
			}
			ran = append(ran, out)
			return padOutputs(prefix, ran, norig), werr
		}
		if allowStale && i == 0 && strings.TrimSpace(out) == "" && shell.stdoutExited() {
			ran = append(ran, out)
			return padOutputs(prefix, ran, norig), io.EOF
		}
		ran = append(ran, out)
	}
	return padOutputs(prefix, ran, norig), nil
}

func (s *pooledSession) runPipeline(ctx context.Context, shell *sshShellSession, cmds []sshCmd, norig int, allowStale bool) ([]string, error) {
	_ = norig
	if len(cmds) == 0 {
		return []string{""}, nil
	}
	lines := make([]string, len(cmds))
	var marker *regexp.Regexp
	for i, c := range cmds {
		lines[i] = c.Cmd
		if c.EndMarker != nil {
			marker = c.EndMarker
		}
	}
	shell.resetCapture()
	if err := shell.write(strings.Join(lines, "\n")); err != nil {
		if allowStale {
			return []string{""}, fmt.Errorf("%w: %v", errStaleWrite, err)
		}
		return []string{""}, err
	}
	werr := shell.waitIdle(ctx, marker)
	out := shell.capture()
	if hasPagerLeftover(out) {
		slog.Info("ssh.pager", "key", s.key, "err", "pager leftover")
		return []string{out}, errPagerLeftover
	}
	if werr != nil {
		if allowStale && strings.TrimSpace(out) == "" && (errors.Is(werr, io.EOF) || errors.Is(werr, errWaitTimeout)) {
			return []string{out}, werr
		}
		return []string{out}, werr
	}
	if allowStale && strings.TrimSpace(out) == "" && shell.stdoutExited() {
		return []string{out}, io.EOF
	}
	return []string{out}, nil
}
