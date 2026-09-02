package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/abundo/factum2/internal/jobscheduler"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

// ApiScheduleList returns every JobSchedule, enabled first then by name -
// the Scheduler page's table.
func (ctrl *Controller) ApiScheduleList(c *echo.Context) error {
	var schedules []models.JobSchedule
	if err := ctrl.DB.Order("enabled desc, name, id").Find(&schedules).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, schedules)
}

// ApiScheduleGet returns one JobSchedule by ID.
func (ctrl *Controller) ApiScheduleGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
	}
	var sched models.JobSchedule
	if err := ctrl.DB.First(&sched, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, sched)
}

// ApiScheduleCreate inserts a JobSchedule and, if enabled, stamps NextRunAt
// from the cron expression so the in-process scheduler can pick it up
// without waiting for an edit.
func (ctrl *Controller) ApiScheduleCreate(c *echo.Context) error {
	var dto models.JobScheduleDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name, target, cronExpr, err := jobscheduler.Validate(dto.Name, dto.Target, dto.Cron)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	sched := models.JobSchedule{
		Name:      name,
		Enabled:   dto.Enabled,
		Target:    target,
		Cron:      cronExpr,
		CreatedBy: ctrl.currentUsername(c),
	}
	if dto.Enabled {
		next, nextErr := jobscheduler.NextRun(cronExpr, time.Now(), jobscheduler.Location())
		if nextErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": nextErr.Error()})
		}
		sched.NextRunAt = &next
	}
	if err := ctrl.DB.Create(&sched).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, sched)
}

// ApiScheduleUpdate replaces the writable fields and recomputes NextRunAt
// from now (or clears it when disabled). LastRunAt/LastError/CreatedBy stay
// as the scheduler last left them.
func (ctrl *Controller) ApiScheduleUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
	}
	var sched models.JobSchedule
	if err := ctrl.DB.First(&sched, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	var dto models.JobScheduleDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	name, target, cronExpr, err := jobscheduler.Validate(dto.Name, dto.Target, dto.Cron)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	fields := map[string]any{
		"name":    name,
		"enabled": dto.Enabled,
		"target":  target,
		"cron":    cronExpr,
	}
	if dto.Enabled {
		next, nextErr := jobscheduler.NextRun(cronExpr, time.Now(), jobscheduler.Location())
		if nextErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": nextErr.Error()})
		}
		fields["next_run_at"] = next
	} else {
		fields["next_run_at"] = nil
	}
	if err := ctrl.DB.Model(&sched).Updates(fields).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.First(&sched, sched.ID).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, sched)
}

// ApiScheduleDelete removes a JobSchedule. In-flight jobs it already
// started keep running - the row is only the trigger, not the Job.
func (ctrl *Controller) ApiScheduleDelete(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
	}
	var sched models.JobSchedule
	if err := ctrl.DB.First(&sched, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, map[string]any{"error": "schedule not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := ctrl.DB.Delete(&sched).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
