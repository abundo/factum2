// Package housekeeping trims persisted job history (Job / JobTask /
// JobTaskEvent) so the tables that receive hub "event" envelopes cannot
// grow without bound. It is a job target, not a background loop: an
// operator schedules or triggers "housekeeping" the same way as a sync
// target; nothing runs it on its own.
package housekeeping

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// DefaultKeep is how many newest finished Jobs Trim retains when the
// caller passes Keep < 1. Matches GET /api/jobs's list cap.
const DefaultKeep = 50

// Result is how many rows Trim deleted. EventsDeleted includes both
// events belonging to deleted jobs and orphan rows (job_task_id IS NULL).
type Result struct {
	JobsDeleted   int64
	TasksDeleted  int64
	EventsDeleted int64
}

// Options controls one Trim run.
type Options struct {
	// Keep is the number of newest finished Jobs to retain, plus their
	// JobTasks and JobTaskEvents. Values < 1 mean DefaultKeep.
	Keep int
	// ProtectJobIDs are never deleted, even if they are finished and
	// outside the Keep window - used to spare the in-flight housekeeping
	// job itself (already unfinished, but listed here as belt-and-braces).
	ProtectJobIDs []uint
}

// EffectiveKeep maps a Settings.JobHistoryKeep value onto the number Trim
// will actually retain. 0/negative is treated as DefaultKeep so an unset
// column cannot wipe history on the first run.
func EffectiveKeep(n int) int {
	if n < 1 {
		return DefaultKeep
	}
	return n
}

// Trim deletes finished Jobs older than the Keep newest, along with their
// JobTasks and JobTaskEvents, and any orphan JobTaskEvent rows (no owning
// JobTask - ad hoc --job runs). Unfinished jobs are never deleted.
func Trim(db *gorm.DB, opt Options) (Result, error) {
	keep := EffectiveKeep(opt.Keep)
	protect := make(map[uint]struct{}, len(opt.ProtectJobIDs))
	for _, id := range opt.ProtectJobIDs {
		if id != 0 {
			protect[id] = struct{}{}
		}
	}

	var result Result
	err := db.Transaction(func(tx *gorm.DB) error {
		var finished []models.Job
		if err := tx.Where("finished_at IS NOT NULL").Order("id desc").Find(&finished).Error; err != nil {
			return err
		}

		kept := 0
		var deleteIDs []uint
		for _, j := range finished {
			if _, ok := protect[j.ID]; ok {
				continue
			}
			if kept < keep {
				kept++
				continue
			}
			deleteIDs = append(deleteIDs, j.ID)
		}

		res := tx.Where("job_task_id IS NULL").Delete(&models.JobTaskEvent{})
		if res.Error != nil {
			return res.Error
		}
		result.EventsDeleted += res.RowsAffected

		if len(deleteIDs) == 0 {
			return nil
		}

		var taskIDs []uint
		if err := tx.Model(&models.JobTask{}).Where("job_id IN ?", deleteIDs).Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) > 0 {
			res = tx.Where("job_task_id IN ?", taskIDs).Delete(&models.JobTaskEvent{})
			if res.Error != nil {
				return res.Error
			}
			result.EventsDeleted += res.RowsAffected
		}

		res = tx.Where("job_id IN ?", deleteIDs).Delete(&models.JobTask{})
		if res.Error != nil {
			return res.Error
		}
		result.TasksDeleted = res.RowsAffected

		res = tx.Where("id IN ?", deleteIDs).Delete(&models.Job{})
		if res.Error != nil {
			return res.Error
		}
		result.JobsDeleted = res.RowsAffected
		return nil
	})
	return result, err
}
