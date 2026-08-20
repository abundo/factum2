package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// ----- API sync -----

// ApiSyncTargets returns the systems that can be synced, so the frontend can
// render one button per target without hardcoding the list - filtered down
// to those enabled in Settings (worker.EnabledSyncTargets), so a source/
// destination an admin hasn't activated doesn't show a Sync button at all.
func (ctrl *Controller) ApiSyncTargets(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, worker.EnabledSyncTargets(settings))
}

// ApiSyncTrigger dispatches :target to exactly one connected worker node
// activated for it (see internal/worker/hub.go's RemoteManager.StartJob -
// deliberately single-node per target, not a fan-out like SendCommand/
// "factum-worker run", so a target maps to exactly one execution) and
// creates a Job row (with one JobTask child) tracking it. Unlike the old
// rabbitmq-based PublishSync, which silently "succeeded" even with nobody
// listening, matched==0 is now an observable fact and reported as an error
// instead - the job/task rows still get created and immediately marked
// failed, so they show up in history rather than just vanishing. StartJob
// also refuses to dispatch a target that already has a task in flight
// (worker.ErrSyncAlreadyRunning), reported here as 409 Conflict - the
// frontend's Sync buttons are expected to already be disabled in this
// case, but the server is the actual enforcement point.
func (ctrl *Controller) ApiSyncTrigger(c *echo.Context) error {
	target := c.Param("target")
	if !worker.IsValidSyncTarget(target) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "unknown sync target"})
	}

	job, results, err := ctrl.RemoteManager.StartJob("sync", ctrl.currentUsername(c), []string{target})
	if err != nil {
		if errors.Is(err, worker.ErrSyncAlreadyRunning) {
			return c.JSON(http.StatusConflict, map[string]any{"error": err.Error(), "target": target})
		}
		slog.Error("dispatch sync request", "target", target, "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to dispatch sync request"})
	}
	task := results[0]
	if task.Matched == 0 {
		return c.JSON(http.StatusBadGateway, map[string]any{
			"error": "no connected worker node handles this sync target",
			"target": target, "job_id": job.ID, "task_id": task.TaskID,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status": "queued", "target": target, "job_id": job.ID, "task_id": task.TaskID,
	})
}

// ApiSyncTriggerAll dispatches every enabled sync target as one Job with a
// JobTask per target (RemoteManager.StartJob) - the server-side successor
// of the old client-side "trigger targets one at a time, waiting for each"
// loop (SyncOverviewPage.vue's syncAll): targets are dispatched one at a
// time, each waited on to finish before the next starts
// (RemoteManager.dispatchRemainingSequentially), in the sources-then-
// destinations order worker.SequencedSyncAllTargets produces, so a
// destination sync never runs while (or ahead of) the source sync that
// feeds it. Unlike ApiSyncTrigger, a single target's conflict/dispatch
// failure doesn't fail the whole request - it's recorded against that
// target's JobTask row instead (see worker.RemoteManager.startOneTask), so
// the response is 200 as long as the parent Job row itself was created.
// The response's per-target "tasks" only ever has (at most) one entry now
// - the first target, dispatched synchronously - since the rest are
// dispatched later from a background goroutine; nothing currently reads
// that field besides job_id (see SyncOverviewPage.vue's syncAll and
// factum.FactumClient.TriggerSyncAll), so its shape was kept rather than
// removed.
func (ctrl *Controller) ApiSyncTriggerAll(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	targets := worker.SequencedSyncAllTargets(worker.EnabledSyncTargets(settings))
	if len(targets) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "no sync targets are enabled"})
	}

	job, results, err := ctrl.RemoteManager.StartJob("sync", ctrl.currentUsername(c), targets)
	if err != nil {
		slog.Error("dispatch sync-all request", "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to dispatch sync request"})
	}

	tasks := make([]map[string]any, 0, len(results))
	for _, r := range results {
		t := map[string]any{"target": r.Target, "task_id": r.TaskID, "matched": r.Matched}
		if r.Err != nil {
			t["error"] = r.Err.Error()
		}
		tasks = append(tasks, t)
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "queued", "job_id": job.ID, "tasks": tasks})
}

// currentUsername extracts the triggering user's username from context, if
// any - shared by ApiSyncTrigger (via StartJob's triggeredBy) and
// ApiSyncTriggerAll.
func (ctrl *Controller) currentUsername(c *echo.Context) string {
	if user, ok := c.Get("user").(models.User); ok {
		return user.Username
	}
	return ""
}

// ApiJobs lists the most recent Job rows, newest first, with their
// JobTasks preloaded in the same query (no N+1 when the frontend opens a
// job's detail modal) - the job status page's job-history table.
func (ctrl *Controller) ApiJobs(c *echo.Context) error {
	var jobs []models.Job
	if err := ctrl.DB.Preload("Tasks").Order("id desc").Limit(50).Find(&jobs).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, jobs)
}

// ApiJobTaskEvents lists a single JobTask's JobTaskEvent rows in
// chronological order - the job-history table's per-subjob drill-in view.
func (ctrl *Controller) ApiJobTaskEvents(c *echo.Context) error {
	jobID := c.Param("id")
	taskID := c.Param("taskid")

	var task models.JobTask
	if err := ctrl.DB.Where("id = ? AND job_id = ?", taskID, jobID).First(&task).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "job task not found"})
	}

	var events []models.JobTaskEvent
	if err := ctrl.DB.Where("job_task_id = ?", task.ID).Order("id asc").Find(&events).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, events)
}
