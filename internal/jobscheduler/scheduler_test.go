package jobscheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := util.MigrateDatabase(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestValidate(t *testing.T) {
	t.Parallel()
	name, target, cronExpr, err := Validate("  Nightly  ", "all", "0 2 * * *")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if name != "Nightly" || target != "all" || cronExpr != "0 2 * * *" {
		t.Fatalf("got %q %q %q", name, target, cronExpr)
	}

	if _, _, _, err := Validate("", "dns", "* * * * *"); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, _, _, err := Validate("x", "not-a-target", "* * * * *"); err == nil {
		t.Fatal("expected error for unknown target")
	}
	if _, _, _, err := Validate("x", "dns", "not cron"); err == nil {
		t.Fatal("expected error for invalid cron")
	}
	if _, _, _, err := Validate("x", "dns", "@hourly"); err != nil {
		t.Fatalf("descriptor should be accepted: %v", err)
	}
	if _, _, _, err := Validate("Nightly trim", "housekeeping", "0 3 * * *"); err != nil {
		t.Fatalf("housekeeping should be a valid schedule target: %v", err)
	}
}

func TestNextRunDaily(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	after := time.Date(2026, 8, 20, 10, 0, 0, 0, loc)
	next, err := NextRun("0 2 * * *", after, loc)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 8, 21, 2, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestTickFiresDueSingleTarget(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)

	sched := models.JobSchedule{
		Name:      "DNS every minute",
		Enabled:   true,
		Target:    "dns",
		Cron:      "* * * * *",
		NextRunAt: &past,
	}
	if err := db.Create(&sched).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var calls []struct {
		jobType, triggeredBy string
		targets              []string
	}
	s := New(db, func(jobType, triggeredBy string, targets []string) error {
		calls = append(calls, struct {
			jobType, triggeredBy string
			targets              []string
		}{jobType, triggeredBy, targets})
		return nil
	})
	s.loc = time.UTC
	s.Tick(now)

	if len(calls) != 1 {
		t.Fatalf("StartJob calls = %d, want 1", len(calls))
	}
	if calls[0].jobType != "sync" || calls[0].triggeredBy != "schedule:DNS every minute" {
		t.Fatalf("call = %+v", calls[0])
	}
	if len(calls[0].targets) != 1 || calls[0].targets[0] != "dns" {
		t.Fatalf("targets = %v, want [dns]", calls[0].targets)
	}

	var got models.JobSchedule
	if err := db.First(&got, sched.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(now) {
		t.Fatalf("LastRunAt = %v, want %v", got.LastRunAt, now)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(now) {
		t.Fatalf("NextRunAt = %v, want after %v", got.NextRunAt, now)
	}
	if got.LastError != "" {
		t.Fatalf("LastError = %q, want empty", got.LastError)
	}

	// A second tick before NextRunAt must not fire again.
	s.Tick(now.Add(time.Second))
	if len(calls) != 1 {
		t.Fatalf("second tick fired extra StartJob, calls = %d", len(calls))
	}
}

func TestTickSkipsDisabledAndFuture(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	if err := db.Create(&models.JobSchedule{Name: "disabled", Enabled: false, Target: "dns", Cron: "* * * * *", NextRunAt: &past}).Error; err != nil {
		t.Fatalf("create disabled: %v", err)
	}
	if err := db.Create(&models.JobSchedule{Name: "future", Enabled: true, Target: "dns", Cron: "* * * * *", NextRunAt: &future}).Error; err != nil {
		t.Fatalf("create future: %v", err)
	}

	calls := 0
	s := New(db, func(string, string, []string) error {
		calls++
		return nil
	})
	s.Tick(now)
	if calls != 0 {
		t.Fatalf("StartJob calls = %d, want 0", calls)
	}
}

func TestTickRecordsStartJobError(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	sched := models.JobSchedule{Name: "busy", Enabled: true, Target: "dns", Cron: "* * * * *", NextRunAt: &past}
	if err := db.Create(&sched).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	s := New(db, func(string, string, []string) error {
		return errors.New("sync already running for this target")
	})
	s.loc = time.UTC
	s.Tick(now)

	var got models.JobSchedule
	if err := db.First(&got, sched.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LastError == "" {
		t.Fatal("expected LastError to be set")
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(now) {
		t.Fatal("NextRunAt should still advance so a busy target is not retried every tick")
	}
}

func TestTickAllUsesEnabledSequencedTargets(t *testing.T) {
	db := newTestDB(t)
	enabled := true
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	settings.NetboxEnabled = &enabled
	settings.DnsEnabled = &enabled
	settings.LimeEnabled = &enabled
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	sched := models.JobSchedule{Name: "nightly", Enabled: true, Target: TargetAll, Cron: "* * * * *", NextRunAt: &past}
	if err := db.Create(&sched).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var gotTargets []string
	s := New(db, func(_, _ string, targets []string) error {
		gotTargets = append([]string{}, targets...)
		return nil
	})
	s.loc = time.UTC
	s.Tick(now)

	// Sources (netbox, lime) before destinations (dns), matching
	// SequencedSyncAllTargets.
	want := []string{"netbox", "lime", "dns"}
	if len(gotTargets) != len(want) {
		t.Fatalf("targets = %v, want %v", gotTargets, want)
	}
	for i := range want {
		if gotTargets[i] != want[i] {
			t.Fatalf("targets = %v, want %v", gotTargets, want)
		}
	}
}

func TestResolveTargetsAllEmpty(t *testing.T) {
	db := newTestDB(t)
	_, err := ResolveTargets(db, TargetAll)
	if err == nil {
		t.Fatal("expected error when no targets are enabled")
	}
}

func TestResolveTargetsHousekeeping(t *testing.T) {
	db := newTestDB(t)
	got, err := ResolveTargets(db, worker.HousekeepingTarget)
	if err != nil {
		t.Fatalf("ResolveTargets(housekeeping): %v", err)
	}
	if len(got) != 1 || got[0] != worker.HousekeepingTarget {
		t.Fatalf("got %v, want [housekeeping]", got)
	}

	enabled := true
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	settings.DnsEnabled = &enabled
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
	all, err := ResolveTargets(db, TargetAll)
	if err != nil {
		t.Fatalf("ResolveTargets(all): %v", err)
	}
	for _, tname := range all {
		if tname == worker.HousekeepingTarget {
			t.Fatal("housekeeping must not be included in schedule target \"all\"")
		}
	}
}
