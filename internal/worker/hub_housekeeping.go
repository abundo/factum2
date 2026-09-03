package worker

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/abundo/factum2/internal/housekeeping"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// runHousekeeping is the in-process body of the housekeeping job target.
// It trims persisted job history, records a JobTaskEvent summarizing the
// result (so the job-history drill-in shows something), and then finishes
// the task the same way a worker's StreamExit would.
func (m *RemoteManager) runHousekeeping(taskID string, jobTaskPK uint) {
	exitCode := 0
	errMsg := ""
	defer func() {
		if r := recover(); r != nil {
			exitCode = -1
			errMsg = fmt.Sprintf("panic: %v", r)
			slog.Error("worker hub: housekeeping panic", "err", errMsg)
		}
		m.finishJobTask(LogMsg{
			ID:       taskID,
			Command:  HousekeepingTarget,
			Stream:   StreamExit,
			ExitCode: exitCode,
			Err:      errMsg,
		})
	}()

	var task models.JobTask
	if err := m.db.First(&task, jobTaskPK).Error; err != nil {
		exitCode = -1
		errMsg = err.Error()
		slog.Error("worker hub: housekeeping load task", "job_task_id", jobTaskPK, "err", err)
		return
	}

	keep := housekeeping.DefaultKeep
	if settings, err := util.GetOrCreateSettings(m.db); err != nil {
		slog.Error("worker hub: housekeeping load settings", "err", err)
	} else if settings.JobHistoryKeep > 0 {
		keep = settings.JobHistoryKeep
	}

	result, err := housekeeping.Trim(m.db, housekeeping.Options{
		Keep:          keep,
		ProtectJobIDs: []uint{task.JobID},
	})
	if err != nil {
		exitCode = 1
		errMsg = err.Error()
		m.recordLocalJobEvent(jobTaskPK, taskID, HousekeepingTarget, "error", err.Error())
		slog.Error("housekeeping failed", "err", err)
		return
	}

	msg := fmt.Sprintf("deleted %d jobs, %d tasks, %d events (keeping last %d finished jobs)",
		result.JobsDeleted, result.TasksDeleted, result.EventsDeleted, keep)
	m.recordLocalJobEvent(jobTaskPK, taskID, HousekeepingTarget, "info", msg)
	slog.Info("housekeeping finished",
		"jobs_deleted", result.JobsDeleted,
		"tasks_deleted", result.TasksDeleted,
		"events_deleted", result.EventsDeleted,
		"keep", keep)
}

func (m *RemoteManager) recordLocalJobEvent(jobTaskPK uint, taskID, target, level, message string) {
	pk := jobTaskPK
	if err := m.db.Create(&models.JobTaskEvent{
		JobTaskID: &pk,
		TaskID:    taskID,
		Target:    target,
		Level:     level,
		Message:   message,
		At:        time.Now(),
	}).Error; err != nil {
		slog.Error("worker hub: persist housekeeping event", "err", err)
		return
	}
	if level != "error" && level != "warning" {
		return
	}
	col := "error_count"
	if level == "warning" {
		col = "warning_count"
	}
	if err := m.db.Model(&models.JobTask{}).Where("id = ?", jobTaskPK).
		UpdateColumn(col, gorm.Expr(col+" + ?", 1)).Error; err != nil {
		slog.Error("worker hub: increment housekeeping event counter", "job_task_id", jobTaskPK, "err", err)
	}
}
