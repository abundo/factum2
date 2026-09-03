package housekeeping

import (
	"fmt"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/util"
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

func seedJob(t *testing.T, db *gorm.DB, taskID, target string, finished bool, orphanEvent bool) models.Job {
	t.Helper()
	now := time.Now()
	job := models.Job{Type: "sync", TriggeredBy: "test", StartedAt: now, ExpectedTasks: 1}
	if finished {
		job.FinishedAt = &now
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	task := models.JobTask{JobID: job.ID, TaskID: taskID, Target: target, StartedAt: now}
	if finished {
		task.FinishedAt = &now
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	pk := task.ID
	ev := models.JobTaskEvent{TaskID: taskID, Target: target, Level: "info", Message: "hello", At: now}
	if !orphanEvent {
		ev.JobTaskID = &pk
	}
	if err := db.Create(&ev).Error; err != nil {
		t.Fatalf("create event: %v", err)
	}
	return job
}

func count(t *testing.T, db *gorm.DB, model any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEffectiveKeep(t *testing.T) {
	t.Parallel()
	if got := EffectiveKeep(0); got != DefaultKeep {
		t.Fatalf("EffectiveKeep(0) = %d, want %d", got, DefaultKeep)
	}
	if got := EffectiveKeep(-3); got != DefaultKeep {
		t.Fatalf("EffectiveKeep(-3) = %d, want %d", got, DefaultKeep)
	}
	if got := EffectiveKeep(10); got != 10 {
		t.Fatalf("EffectiveKeep(10) = %d, want 10", got)
	}
}

func TestTrimKeepsNewestFinishedJobs(t *testing.T) {
	db := newTestDB(t)
	var ids []uint
	for i := 0; i < 5; i++ {
		job := seedJob(t, db, fmt.Sprintf("t%d", i), "dns", true, false)
		ids = append(ids, job.ID)
	}

	result, err := Trim(db, Options{Keep: 2})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if result.JobsDeleted != 3 || result.TasksDeleted != 3 || result.EventsDeleted != 3 {
		t.Fatalf("result = %+v, want 3/3/3", result)
	}

	var remaining []models.Job
	if err := db.Order("id").Find(&remaining).Error; err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(remaining) != 2 || remaining[0].ID != ids[3] || remaining[1].ID != ids[4] {
		t.Fatalf("remaining ids = %v, want [%d %d]", remainingIDs(remaining), ids[3], ids[4])
	}
	if count(t, db, &models.JobTask{}) != 2 || count(t, db, &models.JobTaskEvent{}) != 2 {
		t.Fatalf("tasks=%d events=%d, want 2/2", count(t, db, &models.JobTask{}), count(t, db, &models.JobTaskEvent{}))
	}
}

func TestTrimSkipsUnfinishedAndProtected(t *testing.T) {
	db := newTestDB(t)
	old := seedJob(t, db, "old", "dns", true, false)
	protected := seedJob(t, db, "prot", "dns", true, false)
	unfinished := seedJob(t, db, "run", "housekeeping", false, false)
	newest := seedJob(t, db, "new", "dns", true, false)

	result, err := Trim(db, Options{Keep: 1, ProtectJobIDs: []uint{protected.ID}})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if result.JobsDeleted != 1 {
		t.Fatalf("JobsDeleted = %d, want 1", result.JobsDeleted)
	}

	var remaining []models.Job
	if err := db.Order("id").Find(&remaining).Error; err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	got := remainingIDs(remaining)
	want := []uint{protected.ID, unfinished.ID, newest.ID}
	if len(got) != len(want) {
		t.Fatalf("remaining = %v, want %v (old=%d)", got, want, old.ID)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remaining = %v, want %v", got, want)
		}
	}
}

func TestTrimDeletesOrphanEvents(t *testing.T) {
	db := newTestDB(t)
	seedJob(t, db, "kept", "dns", true, false)
	seedJob(t, db, "orphan", "dns", true, true)

	result, err := Trim(db, Options{Keep: 50})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("EventsDeleted = %d, want 1 (orphan only)", result.EventsDeleted)
	}
	if count(t, db, &models.Job{}) != 2 || count(t, db, &models.JobTaskEvent{}) != 1 {
		t.Fatalf("jobs=%d events=%d, want 2/1", count(t, db, &models.Job{}), count(t, db, &models.JobTaskEvent{}))
	}
}

func TestTrimEmptyIsNoop(t *testing.T) {
	db := newTestDB(t)
	result, err := Trim(db, Options{Keep: 50})
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("result = %+v, want zero", result)
	}
}

func remainingIDs(jobs []models.Job) []uint {
	out := make([]uint, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}
