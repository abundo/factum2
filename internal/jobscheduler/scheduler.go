// Package jobscheduler runs user-defined JobSchedule rows: each due
// schedule triggers the same StartJob path as the Job overview page
// (one sync target, or a sequenced "sync all").
package jobscheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// TargetAll is the JobSchedule.Target sentinel for a sequenced sync of
// every currently-enabled target - the scheduled equivalent of
// POST /api/sync/all.
const TargetAll = "all"

// tickInterval is how often Run looks for due schedules. Cron is
// minute-granularity, so this only bounds how late a run starts after its
// NextRunAt, not how often a job itself can fire.
const tickInterval = 15 * time.Second

// Location is the timezone cron expressions are evaluated in - matches
// ConnectDatabase's Postgres session timezone (Europe/Stockholm).
func Location() *time.Location {
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return time.Local
	}
	return loc
}

// StartJobFunc is the subset of worker.RemoteManager.StartJob the
// scheduler needs - returning only the error so tests can stub it
// without constructing Job/TaskResult values.
type StartJobFunc func(jobType, triggeredBy string, targets []string) error

// Scheduler claims due JobSchedule rows and dispatches them through
// StartJob. One instance is started from web.GUI for the process lifetime.
type Scheduler struct {
	db       *gorm.DB
	startJob StartJobFunc
	loc      *time.Location
	mu       sync.Mutex
}

func New(db *gorm.DB, startJob StartJobFunc) *Scheduler {
	return &Scheduler{db: db, startJob: startJob, loc: Location()}
}

// Run ticks until ctx is cancelled. The first Tick is immediate so a
// restart catch-up-fires anything whose NextRunAt passed while the
// process was down - one run, not one per missed interval.
func (s *Scheduler) Run(ctx context.Context) {
	s.Tick(time.Now())
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.Tick(now)
		}
	}
}

// Tick claims every enabled schedule whose NextRunAt is at or before now
// and dispatches it. NextRunAt is advanced before StartJob so a busy
// target (ErrSyncAlreadyRunning) or a dispatch failure is recorded on
// LastError instead of being retried every tick.
func (s *Scheduler) Tick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []models.JobSchedule
	if err := s.db.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("id").Find(&due).Error; err != nil {
		slog.Error("job scheduler: list due schedules", "err", err)
		return
	}
	for i := range due {
		s.fire(&due[i], now)
	}
}

func (s *Scheduler) fire(sched *models.JobSchedule, now time.Time) {
	next, err := NextRun(sched.Cron, now, s.loc)
	if err != nil {
		slog.Error("job scheduler: invalid cron on due schedule, disabling", "id", sched.ID, "cron", sched.Cron, "err", err)
		s.db.Model(sched).Updates(map[string]any{"enabled": false, "last_error": err.Error(), "next_run_at": nil})
		return
	}

	// Claim by id + still-due NextRunAt rather than timestamp equality:
	// Postgres timestamptz round-trips can disagree with the Go value
	// Find just loaded, which would silently skip the fire. Overlapping
	// Ticks are already serialized by mu; a second process seeing the
	// same due row is not a deployment we support (one factum-web).
	res := s.db.Model(&models.JobSchedule{}).
		Where("id = ? AND enabled = ? AND next_run_at <= ?", sched.ID, true, now).
		Updates(map[string]any{
			"next_run_at": next,
			"last_run_at": now,
			"last_error":  "",
		})
	if res.Error != nil {
		slog.Error("job scheduler: claim schedule", "id", sched.ID, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		return
	}

	targets, err := ResolveTargets(s.db, sched.Target)
	if err != nil {
		s.recordError(sched.ID, err)
		return
	}

	triggeredBy := "schedule:" + sched.Name
	if err := s.startJob("sync", triggeredBy, targets); err != nil {
		s.recordError(sched.ID, err)
		slog.Error("job scheduler: start job", "id", sched.ID, "name", sched.Name, "target", sched.Target, "err", err)
		return
	}
	slog.Info("job scheduler: started job", "id", sched.ID, "name", sched.Name, "target", sched.Target, "next_run_at", next)
}

func (s *Scheduler) recordError(id uint, err error) {
	s.db.Model(&models.JobSchedule{}).Where("id = ?", id).Update("last_error", err.Error())
}

// Validate trims and checks the writable fields. cronExpr must parse as a
// standard 5-field expression (or a robfig descriptor like @hourly).
func Validate(name, target, cronExpr string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	target = strings.TrimSpace(target)
	cronExpr = strings.TrimSpace(cronExpr)
	if name == "" {
		return "", "", "", errors.New("name is required")
	}
	if !IsValidTarget(target) {
		return "", "", "", errors.New("unknown sync target")
	}
	if _, err := cron.ParseStandard(cronExpr); err != nil {
		return "", "", "", fmt.Errorf("invalid cron expression: %w", err)
	}
	return name, target, cronExpr, nil
}

func IsValidTarget(target string) bool {
	return target == TargetAll || worker.IsValidSyncTarget(target)
}

// NextRun is the next activation of expr strictly after `after`, in loc.
func NextRun(expr string, after time.Time, loc *time.Location) (time.Time, error) {
	sched, err := cron.ParseStandard(strings.TrimSpace(expr))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	if loc == nil {
		loc = time.UTC
	}
	next := sched.Next(after.In(loc))
	if next.IsZero() {
		return time.Time{}, errors.New("cron expression has no next run")
	}
	return next, nil
}

// ResolveTargets turns a schedule's Target into the StartJob target list.
// "all" uses the same enabled, sources-first order as ApiSyncTriggerAll;
// a named target is passed through even if currently disabled, matching
// ApiSyncTrigger (which only checks IsValidSyncTarget).
func ResolveTargets(db *gorm.DB, target string) ([]string, error) {
	if target != TargetAll {
		if !worker.IsValidSyncTarget(target) {
			return nil, fmt.Errorf("unknown sync target %q", target)
		}
		return []string{target}, nil
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		return nil, err
	}
	targets := worker.SequencedSyncAllTargets(worker.EnabledSyncTargets(settings))
	if len(targets) == 0 {
		return nil, errors.New("no sync targets are enabled")
	}
	return targets, nil
}
