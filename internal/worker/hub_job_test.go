package worker

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newJobTestDB(t *testing.T) *gorm.DB {
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

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestFormatJobDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1s"},
		{500 * time.Millisecond, "<1s"},
		{time.Second, "1s"},
		{1500 * time.Millisecond, "2s"},
		{12 * time.Second, "12s"},
	}
	for _, tc := range cases {
		if got := formatJobDuration(tc.d); got != tc.want {
			t.Errorf("formatJobDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestResolveTaskLogsJobFinishedOnce(t *testing.T) {
	db := newJobTestDB(t)
	logs := captureSlog(t)
	started := time.Now().Add(-12 * time.Second)

	job := models.Job{Type: "sync", TriggeredBy: "admin", StartedAt: started, ExpectedTasks: 2}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	task1 := models.JobTask{JobID: job.ID, TaskID: "t1", Target: "dns", StartedAt: started}
	task2 := models.JobTask{JobID: job.ID, TaskID: "t2", Target: "icinga", StartedAt: started}
	if err := db.Create(&task1).Error; err != nil {
		t.Fatalf("create task1: %v", err)
	}
	if err := db.Create(&task2).Error; err != nil {
		t.Fatalf("create task2: %v", err)
	}

	m := NewRemoteManager(db)
	m.resolveTask("t1", 0, "")
	if strings.Contains(logs.String(), "job finished") {
		t.Fatalf("logged job finished after first of two tasks:\n%s", logs.String())
	}
	var still models.Job
	if err := db.First(&still, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if still.FinishedAt != nil {
		t.Fatal("job finished_at set before last task")
	}

	m.resolveTask("t2", 0, "")
	got := logs.String()
	if strings.Count(got, "msg=\"job finished\"") != 1 {
		t.Fatalf("job finished log count = %d, want 1:\n%s", strings.Count(got, "msg=\"job finished\""), got)
	}
	for _, want := range []string{
		"job_id=" + strconv.FormatUint(uint64(job.ID), 10),
		"status=success",
		"targets=dns,icinga",
		"triggered_by=admin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "duration=") {
		t.Errorf("log missing duration:\n%s", got)
	}

	if err := db.First(&still, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if still.FinishedAt == nil {
		t.Fatal("job finished_at not set")
	}
}

func TestResolveTaskLogsJobFailed(t *testing.T) {
	db := newJobTestDB(t)
	logs := captureSlog(t)
	started := time.Now().Add(-3 * time.Second)

	job := models.Job{Type: "sync", TriggeredBy: "sched", StartedAt: started, ExpectedTasks: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	task := models.JobTask{
		JobID: job.ID, TaskID: "fail-1", Target: "netbox", StartedAt: started,
		ErrorCount: 2, WarningCount: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	m := NewRemoteManager(db)
	m.resolveTask("fail-1", 1, "boom")
	got := logs.String()
	for _, want := range []string{"status=failed", "errors=2", "warnings=1", "targets=netbox"} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q:\n%s", want, got)
		}
	}
}

func TestStartJobEmptyTargetsLogsJobFinished(t *testing.T) {
	db := newJobTestDB(t)
	logs := captureSlog(t)
	m := NewRemoteManager(db)
	job, results, err := m.StartJob("sync", "admin", nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if results != nil {
		t.Fatalf("results = %+v, want nil", results)
	}
	if job.ID == 0 {
		t.Fatal("expected persisted job")
	}
	got := logs.String()
	if !strings.Contains(got, "msg=\"job finished\"") {
		t.Fatalf("missing job finished log:\n%s", got)
	}
	if !strings.Contains(got, "status=success") {
		t.Errorf("log missing status=success:\n%s", got)
	}
}

func TestStartOneTaskConflictFinishesParentJob(t *testing.T) {
	db := newJobTestDB(t)
	logs := captureSlog(t)
	m := NewRemoteManager(db)

	job := models.Job{Type: "sync", TriggeredBy: "admin", StartedAt: time.Now(), ExpectedTasks: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	m.mu.Lock()
	m.running["dns"] = runningJob{TaskID: "already"}
	m.mu.Unlock()

	_, _, err := m.startOneTask(job.ID, "dns")
	if err == nil {
		t.Fatal("expected conflict")
	}

	var got models.Job
	if err := db.First(&got, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if got.FinishedAt == nil {
		t.Fatal("conflicted last task left parent job running")
	}
	if !strings.Contains(logs.String(), "msg=\"job finished\"") {
		t.Fatalf("missing job finished log:\n%s", logs.String())
	}
}
