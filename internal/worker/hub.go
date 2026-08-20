package worker

// hub.go is the primary-side half of the WebSocket transport that replaces
// rabbitmq for worker command dispatch, log streaming, and sync-triggering.
// See hub_agent.go for the agent-side listener this dials into.
//
// Every hub connection, on both sides, has exactly one writer goroutine
// (runWriter) draining a buffered "outbox" channel - gorilla/websocket
// allows only one concurrent writer per connection, and once command
// dispatch means an HTTP-handler goroutine can push a command into an
// already-open connection from outside its own read loop (RemoteManager.
// SendCommand), while N concurrent runCommand/streamOutput goroutines on
// the agent side can be writing log lines at once, nothing may call
// conn.WriteJSON/WriteMessage directly except that one goroutine.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/abundo/factum2/models"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// HubPath is the single route the agent-side listener (runHubListener)
// exposes, and the path RemoteManager dials.
const HubPath = "/hub"

type EnvelopeType string

const (
	EnvelopeHello   EnvelopeType = "hello"
	EnvelopeCommand EnvelopeType = "command"
	EnvelopeLog     EnvelopeType = "log"
	EnvelopeEvent   EnvelopeType = "event"
)

// Envelope wraps every message exchanged over a hub connection.
type Envelope struct {
	Type    EnvelopeType    `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// HelloMsg is sent by the agent immediately after the connection is
// established - the agent is always the one who knows its own
// hostname/roles, regardless of which side dialed.
type HelloMsg struct {
	Hostname string   `json:"hostname"`
	Roles    []string `json:"roles"`
}

// CommandMsg is sent by the primary (RemoteManager) to run a predefined
// command, routed by the agent's activated role name (CommandMsg.Command).
type CommandMsg struct {
	ID      string   `json:"id"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// LogMsg is sent by the agent while (and after) it runs a command.
type LogMsg struct {
	ID       string `json:"id"`
	Command  string `json:"command"`
	Stream   string `json:"stream"` // StreamStdout, StreamStderr or StreamExit
	Data     string `json:"data,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Err      string `json:"err,omitempty"`
}

const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamExit   = "exit"
)

// EventMsg is a structured info/warning/error line reported by a sync tool
// (internal/jobevent.Reporter) via its predefined command's stdout, when
// invoked with --job. ID/Target mirror LogMsg's ID/Command and are filled
// in by the agent (hub_agent.go's streamOutput) - the subprocess itself
// only ever knows its own Level/Message, not its dispatch ID.
type EventMsg struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// LogToSlog converts an agent's LogMsg into the equivalent slog call -
// called directly by connectOnce for every inbound log envelope, which is
// the entire integration into the web GUI's log window: hubHandler
// (web/logstream.go) already tees every slog record into the LogHub the
// frontend subscribes to.
func LogToSlog(msg LogMsg) {
	if msg.Stream == StreamExit {
		slog.Info("command finished", "id", msg.ID, "command", msg.Command, "exit_code", msg.ExitCode, "err", msg.Err)
		return
	}
	slog.Info(msg.Data, "id", msg.ID, "command", msg.Command, "stream", msg.Stream)
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// NodeStatus is a WorkerNode's live connection state.
type NodeStatus struct {
	Connected bool
	Hostname  string
	Roles     []string
	LastSeen  time.Time
	LastError string
}

const (
	reconcileInterval = 10 * time.Second
	dialBackoffMin    = 1 * time.Second
	dialBackoffMax    = 30 * time.Second

	// outboxSize absorbs a burst of concurrent stdout/stderr lines from
	// several commands running at once without blocking their goroutines.
	outboxSize = 256
	// outboxSendTimeout bounds how long a sender (a runCommand/streamOutput
	// goroutine, or an HTTP-handler goroutine calling SendCommand) can block
	// trying to enqueue onto a stuck/dead connection's outbox.
	outboxSendTimeout = 3 * time.Second
)

// trySend enqueues env for delivery by the connection's runWriter goroutine,
// giving up after outboxSendTimeout - the one thing that stops a stuck or
// dead peer from hanging a caller forever. A timeout here almost always
// means runWriter itself is blocked inside (or has already exited after) a
// single slow WriteJSON call, in which case the connection is already being
// torn down via runWriter's own conn.Close() on write error.
func trySend(outbox chan<- Envelope, env Envelope) bool {
	select {
	case outbox <- env:
		return true
	case <-time.After(outboxSendTimeout):
		return false
	}
}

// runWriter is the only goroutine allowed to call WriteJSON/WriteMessage/
// SetWriteDeadline on conn for the life of the connection - see the package
// doc comment. Started right after a successful dial/upgrade, stopped when
// done is closed by the connection's read loop returning, or when a write
// fails, in which case it closes conn itself to force the read loop to
// unblock and tear the whole connection down (Close is documented safe to
// call concurrently/idempotently).
//
// pingPeriod<=0 disables the keepalive-ping branch (a nil ticker channel
// blocks forever in select) - used by the primary side, which doesn't need
// to self-ping since the agent side already does.
func runWriter(conn *websocket.Conn, outbox <-chan Envelope, done <-chan struct{}, pingPeriod time.Duration) {
	var tickerC <-chan time.Time
	if pingPeriod > 0 {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		tickerC = ticker.C
	}
	for {
		select {
		case <-done:
			return
		case env, ok := <-outbox:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(hubWriteWait))
			if err := conn.WriteJSON(env); err != nil {
				conn.Close()
				return
			}
		case <-tickerC:
			_ = conn.SetWriteDeadline(time.Now().Add(hubWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				conn.Close()
				return
			}
		}
	}
}

// nodeConn is a connected WorkerNode's live, writable handle - only present
// in RemoteManager.conns once its hello has been received.
type nodeConn struct {
	outbox chan Envelope
	roles  []string
}

// runningJob tracks a job-task dispatch that hasn't reported StreamExit
// yet, keyed by target in RemoteManager.running - this is what stops
// StartJob from dispatching a second DNS task while the first is still in
// flight, regardless of whether either belongs to a single-target job or a
// batch ("sync all") job - the per-target "one dispatch in flight"
// invariant is unchanged by jobs gaining subjobs, since two dispatches
// racing the same target was always the actual thing being guarded
// against, not "two dispatches racing the same job". Node is recorded so a
// dropped connection (connectOnce's defer) can find and resolve any tasks
// it was running, rather than leaving the target permanently locked
// because the StreamExit that would have cleared it can never arrive on a
// connection that no longer exists. JobTaskPK lets the EnvelopeEvent
// handler populate JobTaskEvent.JobTaskID without an extra DB round trip.
type runningJob struct {
	TaskID    string
	JobTaskPK uint
	Node      string
}

// ErrSyncAlreadyRunning is returned by StartJob (single-target) or recorded
// against a conflicted task (batch) when target already has an
// undispatched-exit task in flight.
var ErrSyncAlreadyRunning = errors.New("sync already running for this target")

// RemoteManager dials out to every enabled models.WorkerNode and keeps one
// supervised connection per node alive, reconnecting with backoff - the
// primary-side half of the hub transport. internal/worker.Worker's
// runHubListener (hub_agent.go) is the agent-side half.
type RemoteManager struct {
	db *gorm.DB

	mu sync.Mutex
	// status is keyed by WorkerNode.Name, not by connection, so a node's
	// last-known state survives a disconnect/reconnect or a disable/
	// re-enable instead of just disappearing.
	status  map[string]NodeStatus
	active  map[string]context.CancelFunc // running dialLoop goroutines, keyed by WorkerNode.Name
	nodes   map[string]models.WorkerNode  // last-applied snapshot, to detect Address/Token edits
	conns   map[string]*nodeConn          // present only once hello is received, keyed by WorkerNode.Name
	waiters map[string]chan LogMsg        // temporary RunAndWait registrations, keyed by CommandMsg.ID
	running map[string]runningJob         // in-flight sync jobs, keyed by target - see runningJob
}

func NewRemoteManager(db *gorm.DB) *RemoteManager {
	return &RemoteManager{
		db:      db,
		status:  make(map[string]NodeStatus),
		active:  make(map[string]context.CancelFunc),
		nodes:   make(map[string]models.WorkerNode),
		conns:   make(map[string]*nodeConn),
		waiters: make(map[string]chan LogMsg),
		running: make(map[string]runningJob),
	}
}

// Run reconciles the configured WorkerNode set against running dial-loops
// every reconcileInterval, until ctx is cancelled. Matches the lifecycle of
// web.GUI()'s other background loops: launched once with a long-lived
// context and no explicit shutdown path.
func (m *RemoteManager) Run(ctx context.Context) {
	m.reconcile(ctx)
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcile(ctx)
		}
	}
}

func (m *RemoteManager) reconcile(ctx context.Context) {
	var rows []models.WorkerNode
	if err := m.db.Where("enabled = ?", true).Find(&rows).Error; err != nil {
		slog.Error("worker hub: load worker nodes", "err", err)
		return
	}
	enabled := make(map[string]models.WorkerNode, len(rows))
	for _, n := range rows {
		enabled[n.Name] = n
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop dial-loops for nodes that are gone or disabled - status is left
	// in m.status (only dialLoop's own writes touch it) so last-known state
	// survives a disable/re-enable.
	for name, cancel := range m.active {
		if _, ok := enabled[name]; !ok {
			cancel()
			delete(m.active, name)
			delete(m.nodes, name)
		}
	}

	// Start (or restart, on an Address/Token edit) a dial-loop for every
	// enabled node.
	for name, node := range enabled {
		if prev, running := m.nodes[name]; running {
			if prev.Address == node.Address && prev.Token == node.Token {
				continue // unchanged, dial-loop already running
			}
			if cancel, ok := m.active[name]; ok {
				cancel()
			}
		}
		nodeCtx, cancel := context.WithCancel(ctx)
		m.active[name] = cancel
		m.nodes[name] = node
		go m.dialLoop(nodeCtx, node)
	}
}

func (m *RemoteManager) setStatus(name string, s NodeStatus) {
	m.mu.Lock()
	m.status[name] = s
	m.mu.Unlock()
}

// StatusAll returns a snapshot of every node's last-known status, keyed by
// WorkerNode.Name - used by web.ApiWorkerStatus alongside the DB's
// Address/Enabled to build the sync-status page's response.
func (m *RemoteManager) StatusAll() map[string]NodeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]NodeStatus, len(m.status))
	for k, v := range m.status {
		out[k] = v
	}
	return out
}

// sendCommandWithID marshals and fans a command envelope out to every
// connected node whose hello-reported roles include role - mirroring the
// old topic-exchange "every agent activated for a role receives it"
// semantics. Returns how many nodes it was actually queued to (0 if none
// matched, or all were too slow/dead to accept it).
func (m *RemoteManager) sendCommandWithID(id, role string, args []string) (matched int, err error) {
	payload, err := json.Marshal(CommandMsg{ID: id, Command: role, Args: args})
	if err != nil {
		return 0, err
	}
	env := Envelope{Type: EnvelopeCommand, Payload: payload}

	m.mu.Lock()
	var targets []*nodeConn
	for _, nc := range m.conns {
		if slices.Contains(nc.roles, role) {
			targets = append(targets, nc)
		}
	}
	m.mu.Unlock()

	for _, nc := range targets {
		if trySend(nc.outbox, env) {
			matched++
		}
	}
	return matched, nil
}

// SendCommand dispatches a fire-and-forget command to every connected node
// activated for role - used by sync-trigger, which doesn't wait for a
// result. See RunAndWait for the wait-for-completion variant.
func (m *RemoteManager) SendCommand(role string, args []string) (matched int, id string, err error) {
	id, err = newID()
	if err != nil {
		return 0, "", err
	}
	matched, err = m.sendCommandWithID(id, role, args)
	return matched, id, err
}

// dispatchSingle sends to at most one connected node activated for role,
// unlike sendCommandWithID's full fan-out - used by createAndDispatchTask
// so a task maps to exactly one execution and one completion signal.
// Fanning a task-tracked dispatch out to every redundant node sharing a
// role would let two independent runs race to update the same JobTask row.
// Returns the dispatched-to node's name (empty if matched is 0) so the
// caller can track which node is running the task.
func (m *RemoteManager) dispatchSingle(id, role string, args []string) (node string, matched int, err error) {
	payload, err := json.Marshal(CommandMsg{ID: id, Command: role, Args: args})
	if err != nil {
		return "", 0, err
	}
	env := Envelope{Type: EnvelopeCommand, Payload: payload}

	m.mu.Lock()
	var targetName string
	var target *nodeConn
	for name, nc := range m.conns {
		if slices.Contains(nc.roles, role) {
			targetName, target = name, nc
			break
		}
	}
	m.mu.Unlock()

	if target == nil {
		return "", 0, nil
	}
	if !trySend(target.outbox, env) {
		return "", 0, nil
	}
	return targetName, 1, nil
}

// TaskResult is one target's outcome within a StartJob call - Err is set if
// that target's dispatch failed or conflicted with an already-running task
// for the same target (batch jobs record this against the target's
// JobTask row and keep going, rather than aborting the whole request -
// see startOneTask). Matched is 0 if dispatch found no connected node for
// it (also recorded against the JobTask row, via createAndDispatchTask).
type TaskResult struct {
	Target  string
	TaskID  string
	Matched int
	Err     error
}

// reserveTarget atomically checks and reserves target in m.running,
// returning a fresh task ID on success or ErrSyncAlreadyRunning if target
// already has a dispatch in flight - the single place the "one dispatch
// per target" invariant is enforced, shared by both StartJob call shapes
// below.
func (m *RemoteManager) reserveTarget(target string) (taskID string, err error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, busy := m.running[target]; busy {
		return "", fmt.Errorf("%w (task %s)", ErrSyncAlreadyRunning, existing.TaskID)
	}
	m.running[target] = runningJob{TaskID: id}
	return id, nil
}

// createAndDispatchTask creates taskID's JobTask row (target must already
// be reserved in m.running via reserveTarget) as a child of jobPK, and
// dispatches it to exactly one connected node - the row is created
// *before* dispatching, same ordering discipline as RunAndWait's "register
// waiter before sending": nothing (an EnvelopeEvent, the StreamExit line)
// can reference a TaskID that doesn't have a row yet. If dispatchSingle
// matches nobody, the row is immediately finished as failed via
// finishJobTask rather than left "started, never finished".
func (m *RemoteManager) createAndDispatchTask(jobPK uint, taskID, target string) (matched int, err error) {
	task := models.JobTask{
		JobID:     jobPK,
		TaskID:    taskID,
		Target:    target,
		StartedAt: time.Now(),
	}
	if err := m.db.Create(&task).Error; err != nil {
		m.clearRunning(target, taskID)
		return 0, err
	}

	m.mu.Lock()
	if cur, ok := m.running[target]; ok && cur.TaskID == taskID {
		cur.JobTaskPK = task.ID
		m.running[target] = cur
	}
	m.mu.Unlock()

	node, matched, err := m.dispatchSingle(taskID, target, nil)
	if err != nil {
		m.clearRunning(target, taskID)
		return 0, err
	}
	if matched == 0 {
		m.finishJobTask(LogMsg{ID: taskID, Command: target, ExitCode: -1, Err: "no connected worker node handles this target"})
		return 0, nil
	}

	m.mu.Lock()
	if cur, ok := m.running[target]; ok && cur.TaskID == taskID {
		cur.Node = node
		m.running[target] = cur
	}
	m.mu.Unlock()

	return matched, nil
}

// startOneTask is the batch ("sync all") path's per-target unit: reserve,
// create, dispatch via reserveTarget/createAndDispatchTask - except unlike
// a single-target trigger, a busy target doesn't abort the whole batch. It
// gets recorded as an already-failed JobTask instead (still visible in the
// job's task list/tab UI) so the rest of the batch's targets still get
// dispatched. Used only for len(targets)>1 calls to StartJob; the
// len(targets)==1 case inlines reserveTarget/createAndDispatchTask
// directly so a conflict on the lone target leaves nothing persisted at
// all, matching the old single-target StartSyncJob's exact behavior.
func (m *RemoteManager) startOneTask(jobPK uint, target string) (taskID string, matched int, err error) {
	taskID, reserveErr := m.reserveTarget(target)
	if reserveErr != nil {
		id, genErr := newID()
		if genErr != nil {
			return "", 0, genErr
		}
		task := models.JobTask{JobID: jobPK, TaskID: id, Target: target, StartedAt: time.Now()}
		if createErr := m.db.Create(&task).Error; createErr != nil {
			return "", 0, createErr
		}
		if updErr := m.db.Model(&task).Updates(map[string]any{
			"finished_at": time.Now(),
			"exit_code":   -1,
			"err":         reserveErr.Error(),
		}).Error; updErr != nil {
			slog.Error("worker hub: record conflicted job task", "target", target, "err", updErr)
		}
		return id, 0, reserveErr
	}

	matched, err = m.createAndDispatchTask(jobPK, taskID, target)
	return taskID, matched, err
}

// sequentialSyncPollInterval is how often dispatchRemainingSequentially
// polls a dispatched target's JobTask row for completion before moving on
// to the next one - only bounds the gap between successive targets in a
// "Sync all" batch, not how long any individual sync itself takes, so it
// can stay short without meaningfully changing total batch duration.
const sequentialSyncPollInterval = 1 * time.Second

// StartJob creates a Job row and dispatches one JobTask per target - the
// single entry point both a single-target trigger (len(targets)==1) and
// "sync all" (every enabled target) go through, per the "every Job is a
// main job with one or more subjobs" model.
//
// len(targets)==1 preserves exact single-target semantics: a busy target
// returns ErrSyncAlreadyRunning and nothing is persisted at all, not even
// the parent Job row. len(targets)>1 always creates the parent Job row
// first and lets individual targets fail/conflict independently (each
// still gets a visible, failed JobTask row, see startOneTask) rather than
// aborting the whole batch - but unlike a single-target call, targets are
// dispatched one at a time, each waited on to finish before the next
// starts (dispatchRemainingSequentially), so a batch job finishes in
// sum(subjob durations) rather than max(subjob durations). This
// deliberately restores the old client-side "sync all" loop's await-each-
// in-turn behavior, just moved server-side (see web.ApiSyncTriggerAll and
// worker.SequencedSyncAllTargets, which orders targets sources-first so a
// destination sync doesn't run - sequentially or otherwise - ahead of the
// source sync that's supposed to feed it) - two targets actually running
// their sync at the same time was the thing this restores protection
// against, not merely the order they're triggered in.
//
// The first target is dispatched synchronously, so a caller gets an
// immediate TaskResult/error for it exactly as before; the remaining
// targets (if any) are dispatched from a background goroutine the caller
// doesn't wait on, since a batch can take much longer than an HTTP
// request should block for.
func (m *RemoteManager) StartJob(jobType, triggeredBy string, targets []string) (job models.Job, results []TaskResult, err error) {
	if len(targets) == 1 {
		target := targets[0]
		taskID, reserveErr := m.reserveTarget(target)
		if reserveErr != nil {
			return models.Job{}, nil, reserveErr
		}

		job = models.Job{Type: jobType, TriggeredBy: triggeredBy, StartedAt: time.Now(), ExpectedTasks: 1}
		if createErr := m.db.Create(&job).Error; createErr != nil {
			m.clearRunning(target, taskID)
			return models.Job{}, nil, createErr
		}

		matched, dispatchErr := m.createAndDispatchTask(job.ID, taskID, target)
		if dispatchErr != nil {
			return job, nil, dispatchErr
		}
		return job, []TaskResult{{Target: target, TaskID: taskID, Matched: matched}}, nil
	}

	job = models.Job{Type: jobType, TriggeredBy: triggeredBy, StartedAt: time.Now(), ExpectedTasks: len(targets)}
	if createErr := m.db.Create(&job).Error; createErr != nil {
		return models.Job{}, nil, createErr
	}

	if len(targets) == 0 {
		now := time.Now()
		m.db.Model(&models.Job{}).Where("id = ?", job.ID).Update("finished_at", now)
		return job, nil, nil
	}

	taskID, matched, taskErr := m.startOneTask(job.ID, targets[0])
	results = []TaskResult{{Target: targets[0], TaskID: taskID, Matched: matched, Err: taskErr}}

	if len(targets) > 1 {
		go m.dispatchRemainingSequentially(job.ID, taskID, targets[1:])
	}

	return job, results, nil
}

// dispatchRemainingSequentially is StartJob's multi-target continuation,
// run in its own goroutine: wait for the target already dispatched by
// StartJob (or the previous loop iteration) to finish, then dispatch the
// next one, in order - so "Sync all" runs its targets one at a time
// instead of all at once. Errors from startOneTask (a busy target's
// conflict) are already recorded against that target's JobTask row by
// startOneTask itself, same as the old all-at-once path, so they're only
// logged here, not treated as fatal to the rest of the batch.
func (m *RemoteManager) dispatchRemainingSequentially(jobID uint, firstTaskID string, targets []string) {
	m.waitForTaskFinished(firstTaskID)
	for _, target := range targets {
		taskID, _, err := m.startOneTask(jobID, target)
		if err != nil {
			slog.Error("worker hub: dispatch next sequential sync target", "job_id", jobID, "target", target, "err", err)
		}
		m.waitForTaskFinished(taskID)
	}
}

// waitForTaskFinished polls taskID's JobTask row until FinishedAt is set -
// see sequentialSyncPollInterval. taskID is always non-empty in practice
// (startOneTask/reserveTarget only fail to produce one on a newID()
// error), but an empty ID is treated as already-finished rather than
// polling forever on a row that will never exist.
func (m *RemoteManager) waitForTaskFinished(taskID string) {
	if taskID == "" {
		return
	}
	for {
		var task models.JobTask
		if err := m.db.Select("finished_at").Where("task_id = ?", taskID).First(&task).Error; err != nil {
			slog.Error("worker hub: poll job task", "task_id", taskID, "err", err)
			return
		}
		if task.FinishedAt != nil {
			return
		}
		time.Sleep(sequentialSyncPollInterval)
	}
}

// clearRunning releases target's m.running reservation, but only if it's
// still the one held by taskID - a late/superseded signal for a task
// that's already been resolved must not clear a newer task's reservation.
func (m *RemoteManager) clearRunning(target, taskID string) {
	m.mu.Lock()
	if cur, ok := m.running[target]; ok && cur.TaskID == taskID {
		delete(m.running, target)
	}
	m.mu.Unlock()
}

// resolveTask records one JobTask's completion (exit code/err/finished_at)
// and, if it was the last of its siblings to finish, stamps the parent
// Job's FinishedAt too - checked here (event-driven), not computed at read
// time, since GET /api/jobs is polled every few seconds and the value only
// actually changes once per job's life. "Last sibling" can't just mean "no
// unfinished rows exist" any more: StartJob's sequential batch path creates
// each target's JobTask row only when that target's turn comes up, so
// early on there may be zero unfinished rows simply because most targets'
// rows don't exist yet. Comparing the finished-row count against the Job's
// ExpectedTasks (set once at creation) tells the two cases apart. The
// parent update is guarded by WHERE finished_at IS NULL so two sibling
// tasks finishing concurrently can't race to double-write it. A harmless
// no-op for any taskID that isn't tracked (e.g. a plain "factum-worker
// run" invocation, which never goes through StartJob).
func (m *RemoteManager) resolveTask(taskID string, exitCode int, errMsg string) {
	var task models.JobTask
	if err := m.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("worker hub: load job task", "task_id", taskID, "err", err)
		}
		return
	}

	now := time.Now()
	if err := m.db.Model(&models.JobTask{}).Where("id = ?", task.ID).Updates(map[string]any{
		"finished_at": now,
		"exit_code":   exitCode,
		"err":         errMsg,
	}).Error; err != nil {
		slog.Error("worker hub: resolve job task", "task_id", taskID, "err", err)
		return
	}

	var job models.Job
	if err := m.db.Select("id", "expected_tasks").Where("id = ?", task.JobID).First(&job).Error; err != nil {
		slog.Error("worker hub: load job", "job_id", task.JobID, "err", err)
		return
	}

	var total, remaining int64
	if err := m.db.Model(&models.JobTask{}).Where("job_id = ?", task.JobID).Count(&total).Error; err != nil {
		slog.Error("worker hub: count job tasks", "job_id", task.JobID, "err", err)
		return
	}
	if err := m.db.Model(&models.JobTask{}).
		Where("job_id = ? AND finished_at IS NULL", task.JobID).
		Count(&remaining).Error; err != nil {
		slog.Error("worker hub: count remaining job tasks", "job_id", task.JobID, "err", err)
		return
	}
	if remaining == 0 && total >= int64(job.ExpectedTasks) {
		if err := m.db.Model(&models.Job{}).
			Where("id = ? AND finished_at IS NULL", task.JobID).
			Update("finished_at", now).Error; err != nil {
			slog.Error("worker hub: finish job", "job_id", task.JobID, "err", err)
		}
	}
}

// finishJobTask records a dispatched command's completion against its
// JobTask row (see resolveTask) and releases the target's m.running
// reservation (msg.Command carries the target/role name - see LogMsg), so
// the target becomes dispatchable again the moment this "Done" signal
// arrives, whether that's a real StreamExit from the agent or the
// immediate synthetic failure createAndDispatchTask builds when nobody's
// connected.
func (m *RemoteManager) finishJobTask(msg LogMsg) {
	m.resolveTask(msg.ID, msg.ExitCode, msg.Err)
	m.clearRunning(msg.Command, msg.ID)
}

// failRunningOnNode resolves (as failed) every tracked task that was
// dispatched to node, and releases its m.running reservation - called from
// connectOnce's teardown defer when a connection drops. Without this, a
// task whose agent connection dies mid-run would leave its target locked
// forever: the StreamExit that would normally call finishJobTask can never
// arrive, since it would have to travel over a connection that no longer
// exists (the agent side may well still be running the command to
// completion per its own process-lifetime context, but has nothing left to
// report the result to - see runCommand's doc comment).
func (m *RemoteManager) failRunningOnNode(node string) {
	m.mu.Lock()
	var stale []runningJob
	for target, job := range m.running {
		if job.Node == node {
			stale = append(stale, job)
			delete(m.running, target)
		}
	}
	m.mu.Unlock()

	for _, job := range stale {
		m.resolveTask(job.TaskID, -1, "worker node disconnected before the sync finished")
	}
}

// deliverToWaiter forwards msg to a registered RunAndWait caller, if any -
// called from connectOnce's read loop for every inbound log envelope, so it
// must never block: a slow/dead HTTP client on the other end of the waiter
// channel must not stall ingestion of other nodes' traffic.
func (m *RemoteManager) deliverToWaiter(msg LogMsg) {
	m.mu.Lock()
	ch, ok := m.waiters[msg.ID]
	m.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// RunAndWait sends role/args and blocks until the first matching StreamExit
// line arrives, ctx is cancelled, or zero nodes matched - mirroring the old
// Worker.RunAndWait's "wait for the first responder" semantics (multiple
// agents can be bound to the same role).
//
// The waiter is registered *before* the command is actually sent - not
// after - since a fast agent response could otherwise arrive before
// anything is listening for it, and deliverToWaiter silently drops a
// message with no registered waiter.
func (m *RemoteManager) RunAndWait(ctx context.Context, role string, args []string, onLine func(LogMsg)) (exitCode int, err error) {
	id, err := newID()
	if err != nil {
		return -1, err
	}

	ch := make(chan LogMsg, 16)
	m.mu.Lock()
	m.waiters[id] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.waiters, id)
		m.mu.Unlock()
	}()

	matched, err := m.sendCommandWithID(id, role, args)
	if err != nil {
		return -1, err
	}
	if matched == 0 {
		return -1, fmt.Errorf("no connected worker node handles role %q", role)
	}

	for {
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case msg := <-ch:
			onLine(msg)
			if msg.Stream == StreamExit {
				if msg.Err != "" {
					return msg.ExitCode, fmt.Errorf("%s", msg.Err)
				}
				return msg.ExitCode, nil
			}
		}
	}
}

// dialLoop holds one supervised connection to a single WorkerNode, retrying
// with exponential backoff (capped at dialBackoffMax, reset after any
// connection that got as far as a hello) until nodeCtx is cancelled by
// reconcile (node removed/disabled/edited) or Run's parent ctx.
func (m *RemoteManager) dialLoop(nodeCtx context.Context, node models.WorkerNode) {
	backoff := dialBackoffMin
	for {
		if nodeCtx.Err() != nil {
			return
		}

		connected, err := m.connectOnce(nodeCtx, node)
		if nodeCtx.Err() != nil {
			return
		}
		if err != nil {
			slog.Debug("worker hub: connection ended", "node", node.Name, "address", node.Address, "err", err)
		}
		if connected {
			backoff = dialBackoffMin
		} else {
			backoff *= 2
			if backoff > dialBackoffMax {
				backoff = dialBackoffMax
			}
		}

		select {
		case <-nodeCtx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// connectOnce dials node once and blocks reading frames until the
// connection drops or nodeCtx is cancelled. The returned bool reports
// whether a hello was ever received on this connection, so dialLoop only
// resets its backoff after a real connection, not a bare dial failure.
func (m *RemoteManager) connectOnce(nodeCtx context.Context, node models.WorkerNode) (connected bool, err error) {
	u := url.URL{Scheme: "ws", Host: node.Address, Path: HubPath}
	header := http.Header{"Authorization": {"Bearer " + node.Token}}

	conn, _, dialErr := websocket.DefaultDialer.DialContext(nodeCtx, u.String(), header)
	if dialErr != nil {
		m.setStatus(node.Name, NodeStatus{Connected: false, LastError: dialErr.Error(), LastSeen: time.Now()})
		return false, dialErr
	}
	defer conn.Close()

	outbox := make(chan Envelope, outboxSize)
	done := make(chan struct{})
	defer close(done)
	go runWriter(conn, outbox, done, 0) // primary doesn't self-ping - the agent does

	nc := &nodeConn{outbox: outbox}
	defer func() {
		m.mu.Lock()
		if cur, ok := m.conns[node.Name]; ok && cur == nc {
			delete(m.conns, node.Name)
		}
		m.mu.Unlock()
		m.failRunningOnNode(node.Name)
	}()

	// The gorilla/websocket connection has no context awareness of its
	// own, so nodeCtx cancellation (node removed/disabled/edited) has to be
	// turned into a Close() to unblock the ReadMessage loop below.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-nodeCtx.Done():
			conn.Close()
		case <-stopWatch:
		}
	}()

	for {
		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			m.setStatus(node.Name, NodeStatus{Connected: false, LastError: readErr.Error(), LastSeen: time.Now()})
			return connected, readErr
		}

		var env Envelope
		if unmarshalErr := json.Unmarshal(data, &env); unmarshalErr != nil {
			slog.Error("worker hub: invalid envelope, discarding", "node", node.Name, "err", unmarshalErr)
			continue
		}

		switch env.Type {
		case EnvelopeHello:
			var hello HelloMsg
			if unmarshalErr := json.Unmarshal(env.Payload, &hello); unmarshalErr != nil {
				slog.Error("worker hub: invalid hello envelope, discarding", "node", node.Name, "err", unmarshalErr)
				continue
			}
			connected = true
			nc.roles = hello.Roles
			m.mu.Lock()
			m.conns[node.Name] = nc
			m.mu.Unlock()
			m.setStatus(node.Name, NodeStatus{
				Connected: true,
				Hostname:  hello.Hostname,
				Roles:     hello.Roles,
				LastSeen:  time.Now(),
			})
		case EnvelopeLog:
			var logMsg LogMsg
			if unmarshalErr := json.Unmarshal(env.Payload, &logMsg); unmarshalErr != nil {
				slog.Error("worker hub: invalid log envelope, discarding", "node", node.Name, "err", unmarshalErr)
				continue
			}
			LogToSlog(logMsg)
			m.deliverToWaiter(logMsg)
			if logMsg.Stream == StreamExit {
				m.finishJobTask(logMsg)
			}
		case EnvelopeEvent:
			var eventMsg EventMsg
			if unmarshalErr := json.Unmarshal(env.Payload, &eventMsg); unmarshalErr != nil {
				slog.Error("worker hub: invalid event envelope, discarding", "node", node.Name, "err", unmarshalErr)
				continue
			}

			var jobTaskID *uint
			m.mu.Lock()
			if running, ok := m.running[eventMsg.Target]; ok && running.TaskID == eventMsg.ID && running.JobTaskPK != 0 {
				pk := running.JobTaskPK
				jobTaskID = &pk
			}
			m.mu.Unlock()

			if err := m.db.Create(&models.JobTaskEvent{
				JobTaskID: jobTaskID,
				TaskID:    eventMsg.ID,
				Target:    eventMsg.Target,
				Level:     eventMsg.Level,
				Message:   eventMsg.Message,
				At:        time.Now(),
			}).Error; err != nil {
				slog.Error("worker hub: persist job task event", "node", node.Name, "err", err)
			}

			if jobTaskID != nil {
				var col string
				switch eventMsg.Level {
				case "error":
					col = "error_count"
				case "warning":
					col = "warning_count"
				}
				if col != "" {
					if err := m.db.Model(&models.JobTask{}).Where("id = ?", *jobTaskID).
						UpdateColumn(col, gorm.Expr(col+" + ?", 1)).Error; err != nil {
						slog.Error("worker hub: increment job task event counter", "job_task_id", *jobTaskID, "err", err)
					}
				}
			}
		default:
			slog.Debug("worker hub: unhandled envelope type", "node", node.Name, "type", env.Type)
		}
	}
}
