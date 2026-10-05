package drivers

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// errSSHExecCreds is returned when a new capture wants a different password
// while another capture still holds the cached connection.
var errSSHExecCreds = errors.New("ssh credentials changed while a capture is using the connection")

// sshExecSlot is one cached SSH connection used for raw exec channels
// (packet capture's tcpdump). The interactive CLI pool is a PTY shell and
// cannot carry pcap bytes, and holding it for a whole capture would block
// every other command to that device. This cache keeps the TCP connection
// and the SSH handshake. Each capture opens and closes only an exec channel.
type sshExecSlot struct {
	mu       sync.Mutex
	cond     *sync.Cond
	dialing  bool
	client   *ssh.Client
	password string
	refs     int
	lastUsed time.Time
	stop     chan struct{}
	stopOnce *sync.Once
}

var (
	sshExecMu    sync.Mutex
	sshExecSlots = map[string]*sshExecSlot{}
	sshExecIdle  sync.Once
)

func sshExecKey(username, addr string) string {
	return username + "@" + addr
}

func sshExecSlotFor(username, addr string) *sshExecSlot {
	key := sshExecKey(username, addr)
	sshExecMu.Lock()
	defer sshExecMu.Unlock()
	slot := sshExecSlots[key]
	if slot == nil {
		slot = &sshExecSlot{}
		slot.cond = sync.NewCond(&slot.mu)
		sshExecSlots[key] = slot
	}
	sshExecIdle.Do(func() { go sshExecIdleLoop() })
	return slot
}

func sshExecIdleLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		sshExecMu.Lock()
		slots := make([]*sshExecSlot, 0, len(sshExecSlots))
		for _, slot := range sshExecSlots {
			slots = append(slots, slot)
		}
		sshExecMu.Unlock()
		for _, slot := range slots {
			slot.expire(defaultIdleTimeout)
		}
	}
}

func (slot *sshExecSlot) expire(idle time.Duration) {
	slot.mu.Lock()
	var stale *ssh.Client
	if slot.client != nil && slot.refs == 0 && !slot.lastUsed.IsZero() && time.Since(slot.lastUsed) >= idle {
		stale = slot.takeClientLocked()
	}
	slot.mu.Unlock()
	if stale != nil {
		stale.Close()
	}
}

// takeClientLocked detaches the cached client. The caller closes it
// without holding slot.mu: Close unblocks client.Wait, which takes the lock.
func (slot *sshExecSlot) takeClientLocked() *ssh.Client {
	client := slot.client
	slot.client = nil
	if slot.stopOnce != nil && slot.stop != nil {
		slot.stopOnce.Do(func() { close(slot.stop) })
	}
	slot.stop = nil
	slot.stopOnce = nil
	return client
}

// acquire returns a live client and a release that drops one reference.
// The connection stays cached when the last reference is dropped.
func (slot *sshExecSlot) acquire(username, password, addr string) (*ssh.Client, func(), error) {
	for {
		slot.mu.Lock()
		if slot.client != nil && !passwordEqual(slot.password, password) {
			if slot.refs > 0 {
				slot.mu.Unlock()
				return nil, nil, errSSHExecCreds
			}
			stale := slot.takeClientLocked()
			slot.mu.Unlock()
			if stale != nil {
				stale.Close()
			}
			continue
		}
		if slot.client != nil {
			slot.refs++
			slot.lastUsed = time.Now()
			client := slot.client
			slot.mu.Unlock()
			slog.Info("ssh.exec", "addr", addr, "reused", true)
			return client, slot.release, nil
		}
		if slot.dialing {
			slot.cond.Wait()
			slot.mu.Unlock()
			continue
		}
		slot.dialing = true
		slot.mu.Unlock()

		client, stop, once, err := dialSSHExecClient(username, password, addr)
		slot.mu.Lock()
		slot.dialing = false
		if err != nil {
			slot.cond.Broadcast()
			slot.mu.Unlock()
			return nil, nil, err
		}
		if slot.client != nil && passwordEqual(slot.password, password) {
			slot.refs++
			slot.lastUsed = time.Now()
			existing := slot.client
			slot.cond.Broadcast()
			slot.mu.Unlock()
			once.Do(func() { close(stop) })
			client.Close()
			slog.Info("ssh.exec", "addr", addr, "reused", true)
			return existing, slot.release, nil
		}
		if slot.client != nil && slot.refs > 0 {
			slot.cond.Broadcast()
			slot.mu.Unlock()
			once.Do(func() { close(stop) })
			client.Close()
			return nil, nil, errSSHExecCreds
		}
		var stale *ssh.Client
		if slot.client != nil {
			stale = slot.takeClientLocked()
		}
		slot.client = client
		slot.password = password
		slot.stop = stop
		slot.stopOnce = once
		slot.refs++
		slot.lastUsed = time.Now()
		slot.cond.Broadcast()
		slot.mu.Unlock()
		if stale != nil {
			stale.Close()
		}
		go func() {
			_ = client.Wait()
			once.Do(func() { close(stop) })
			slot.mu.Lock()
			if slot.client == client {
				slot.client = nil
			}
			slot.mu.Unlock()
		}()
		go keepSSHExecAlive(client, stop)
		slog.Info("ssh.exec", "addr", addr, "reused", false)
		return client, slot.release, nil
	}
}

func (slot *sshExecSlot) release() {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.refs > 0 {
		slot.refs--
	}
	slot.lastUsed = time.Now()
}

// discard closes client when it is still the cached connection and nobody
// is using it. A failed NewSession means that connection is gone. A holder
// that is still inside Start drops it after its own release.
func (slot *sshExecSlot) discard(client *ssh.Client) {
	slot.mu.Lock()
	var stale *ssh.Client
	if slot.client == client && slot.refs == 0 {
		stale = slot.takeClientLocked()
	}
	slot.mu.Unlock()
	if stale != nil {
		stale.Close()
	}
}

func dialSSHExecClient(username, password, addr string) (*ssh.Client, chan struct{}, *sync.Once, error) {
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: defaultKeepalive}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, nil, nil, err
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshClientConfig(username, password))
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	return ssh.NewClient(ncc, chans, reqs), make(chan struct{}), &sync.Once{}, nil
}

func keepSSHExecAlive(client *ssh.Client, stop <-chan struct{}) {
	t := time.NewTicker(defaultKeepalive)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ok, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				client.Close()
				return
			}
			if !ok {
				// The server has no OpenSSH keepalive. The TCP keepalive
				// on the dialer still covers the connection.
				return
			}
		}
	}
}

// resetSSHExecCache closes every cached exec connection. Tests use it.
func resetSSHExecCache() {
	sshExecMu.Lock()
	slots := sshExecSlots
	sshExecSlots = map[string]*sshExecSlot{}
	sshExecMu.Unlock()
	for _, slot := range slots {
		slot.mu.Lock()
		client := slot.takeClientLocked()
		slot.mu.Unlock()
		if client != nil {
			client.Close()
		}
	}
}

// openCachedSSHExec runs cmd on a cached SSH connection. Cancelling ctx
// closes the exec channel and stops the remote command. The connection
// stays cached for the next capture.
func openCachedSSHExec(ctx context.Context, username, password, addr, cmd string) (*sshExecStream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slot := sshExecSlotFor(username, addr)
	client, release, err := slot.acquire(username, password, addr)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	stream, err := startExecOnClient(client, cmd)
	if err != nil {
		release()
		if !errors.Is(err, errSSHExecSession) {
			return nil, err
		}
		slot.discard(client)
		client, release, err = slot.acquire(username, password, addr)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		stream, err = startExecOnClient(client, cmd)
		if err != nil {
			release()
			if errors.Is(err, errSSHExecSession) {
				slot.discard(client)
			}
			return nil, err
		}
	}
	stream.release = release
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()
	go func() {
		_ = stream.session.Wait()
	}()
	return stream, nil
}

// errSSHExecSession means NewSession failed and the cached connection
// should be dropped.
var errSSHExecSession = errors.New("ssh exec session failed")

func startExecOnClient(client *ssh.Client, cmd string) (*sshExecStream, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, errSSHExecSession
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, err
	}
	stream := &sshExecStream{session: session, stdout: stdout}
	go func() {
		buf := make([]byte, 1024)
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				stream.writeStderr(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()
	return stream, nil
}
