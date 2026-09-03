package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/housekeeping"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newHousekeepingTestDB(t *testing.T) *gorm.DB {
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

func waitFinished(t *testing.T, db *gorm.DB, taskID string) models.JobTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var task models.JobTask
		if err := db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if task.FinishedAt != nil {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not finish", taskID)
	return models.JobTask{}
}

func TestIsValidJobTarget(t *testing.T) {
	t.Parallel()
	if !IsValidJobTarget(HousekeepingTarget) {
		t.Fatal("housekeeping should be a valid job target")
	}
	if IsValidSyncTarget(HousekeepingTarget) {
		t.Fatal("housekeeping must not be a sync target (Sync all / EnabledSyncTargets)")
	}
	if !IsValidJobTarget("dns") || !IsValidSyncTarget("dns") {
		t.Fatal("dns should remain a valid sync and job target")
	}
}

func TestStartJobHousekeepingRunsLocally(t *testing.T) {
	db := newHousekeepingTestDB(t)
	now := time.Now()
	for i := 0; i < 4; i++ {
		job := models.Job{Type: "sync", TriggeredBy: "seed", StartedAt: now, ExpectedTasks: 1, FinishedAt: &now}
		if err := db.Create(&job).Error; err != nil {
			t.Fatalf("seed job: %v", err)
		}
		task := models.JobTask{
			JobID: job.ID, TaskID: fmt.Sprintf("seed-%d", i), Target: "dns",
			StartedAt: now, FinishedAt: &now,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("seed task: %v", err)
		}
		pk := task.ID
		if err := db.Create(&models.JobTaskEvent{
			JobTaskID: &pk, TaskID: task.TaskID, Target: "dns",
			Level: "info", Message: "seed", At: now,
		}).Error; err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	settings.JobHistoryKeep = 2
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}

	m := NewRemoteManager(db)
	job, results, err := m.StartJob("sync", "test", []string{HousekeepingTarget})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if len(results) != 1 || results[0].Matched != 1 {
		t.Fatalf("results = %+v, want matched=1 without a worker", results)
	}

	task := waitFinished(t, db, results[0].TaskID)
	if task.ExitCode != 0 {
		t.Fatalf("exit_code = %d err=%q, want 0", task.ExitCode, task.Err)
	}

	var jobs []models.Job
	if err := db.Order("id").Find(&jobs).Error; err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	// 2 newest finished seeds kept + the housekeeping job itself.
	if len(jobs) != 3 {
		t.Fatalf("jobs remaining = %d, want 3", len(jobs))
	}
	if jobs[len(jobs)-1].ID != job.ID {
		t.Fatalf("newest job id = %d, want housekeeping %d", jobs[len(jobs)-1].ID, job.ID)
	}

	var events []models.JobTaskEvent
	if err := db.Where("job_task_id = ?", task.ID).Find(&events).Error; err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("housekeeping events = %d, want 1", len(events))
	}
	if events[0].Level != "info" {
		t.Fatalf("event level = %q", events[0].Level)
	}

	var eventCount int64
	if err := db.Model(&models.JobTaskEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	// 2 kept seed events + 1 housekeeping summary.
	if eventCount != 3 {
		t.Fatalf("events remaining = %d, want 3", eventCount)
	}

	if housekeeping.EffectiveKeep(settings.JobHistoryKeep) != 2 {
		t.Fatalf("keep setting not applied")
	}
}
