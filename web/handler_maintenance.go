package web

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func (ctrl *Controller) ApiMaintenanceList(c *echo.Context) error {
	var rows []models.MaintenanceWindow
	if err := ctrl.DB.Order("starts_at desc").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiMaintenanceGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var w models.MaintenanceWindow
	if err := ctrl.DB.First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	impact, _ := optical.ResourceImpact(ctrl.DB, w.ResourceType, w.ResourceID)
	var notes []models.MaintenanceNotification
	_ = ctrl.DB.Where("window_id = ?", w.ID).Find(&notes).Error
	return c.JSON(http.StatusOK, map[string]any{"window": w, "impact": impact, "notifications": notes})
}

func (ctrl *Controller) ApiMaintenanceCreate(c *echo.Context) error {
	var w models.MaintenanceWindow
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	w.ID = 0
	if w.Status == "" {
		w.Status = models.MaintDraft
	}
	if w.Status != models.MaintDraft && w.Status != models.MaintPlanned {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "create as draft or planned"})
	}
	if w.Title == "" || w.ResourceType == "" || w.ResourceID == 0 || w.StartsAt.IsZero() {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "title, resource and starts_at required"})
	}
	if !w.EndsAt.IsZero() && !w.EndsAt.After(w.StartsAt) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "ends_at must be after starts_at"})
	}
	if err := ctrl.maintenanceResourceExists(w.ResourceType, w.ResourceID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if user, ok := c.Get("user").(models.User); ok {
		w.CreatedBy = user.ID
	}
	if err := ctrl.DB.Create(&w).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, w)
}

func (ctrl *Controller) maintenanceResourceExists(kind string, id uint) error {
	var n int64
	switch kind {
	case models.MaintResourceConnection:
		ctrl.DB.Model(&models.Connection{}).Where("id = ?", id).Count(&n)
	case models.MaintResourceDevice:
		ctrl.DB.Model(&models.Device{}).Where("id = ?", id).Count(&n)
	case models.MaintResourceInterface:
		ctrl.DB.Model(&models.Interface{}).Where("id = ?", id).Count(&n)
	default:
		return fmt.Errorf("invalid resource_type")
	}
	if n == 0 {
		return fmt.Errorf("resource not found")
	}
	return nil
}

func (ctrl *Controller) ApiMaintenanceUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var w models.MaintenanceWindow
	if err := ctrl.DB.First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var dto models.MaintenanceWindow
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.Status != "" && dto.Status != w.Status {
		if !maintenanceTransitionOK(w.Status, dto.Status) {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "illegal status transition"})
		}
		w.Status = dto.Status
	}
	if dto.Title != "" {
		w.Title = dto.Title
	}
	if dto.Description != "" {
		w.Description = dto.Description
	}
	if !dto.StartsAt.IsZero() {
		w.StartsAt = dto.StartsAt
	}
	if !dto.EndsAt.IsZero() {
		w.EndsAt = dto.EndsAt
	}
	if err := ctrl.DB.Save(&w).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, w)
}

func maintenanceTransitionOK(from, to string) bool {
	legal := map[string][]string{
		models.MaintDraft:      {models.MaintPlanned, models.MaintCancelled},
		models.MaintPlanned:    {models.MaintNotified, models.MaintInProgress, models.MaintCancelled},
		models.MaintNotified:   {models.MaintNotified, models.MaintInProgress, models.MaintCancelled},
		models.MaintInProgress: {models.MaintCompleted, models.MaintCancelled},
	}
	for _, s := range legal[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (ctrl *Controller) ApiMaintenanceImpact(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var w models.MaintenanceWindow
	if err := ctrl.DB.First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	impact, err := optical.ResourceImpact(ctrl.DB, w.ResourceType, w.ResourceID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, impact)
}

type maintenanceNotifyRequest struct {
	Force bool `json:"force"`
}

func (ctrl *Controller) ApiMaintenanceNotify(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var w models.MaintenanceWindow
	if err := ctrl.DB.First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if w.Status != models.MaintPlanned && w.Status != models.MaintNotified {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "notify only from planned or notified"})
	}
	var req maintenanceNotifyRequest
	_ = c.Bind(&req)

	impact, err := optical.ResourceImpact(ctrl.DB, w.ResourceType, w.ResourceID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	byCustomer := map[uint][]optical.ImpactRow{}
	for _, row := range impact {
		byCustomer[row.CustomerID] = append(byCustomer[row.CustomerID], row)
	}
	if len(byCustomer) == 0 {
		return c.JSON(http.StatusConflict, map[string]any{"error": "no affected services to notify"})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	smtp := util.NewCommonConfig(settings)
	from := settings.EmailSender

	sent := 0
	failed := 0
	for customerID, rows := range byCustomer {
		var links []models.CustomerContact
		ctrl.DB.Where("customer_id = ?", customerID).Find(&links)
		var contacts []models.Contact
		if len(links) > 0 {
			cids := make([]uint, len(links))
			for i, l := range links {
				cids[i] = l.ContactID
			}
			ctrl.DB.Where("id IN ? AND notify_maintenance = ? AND email <> ''", cids, true).Find(&contacts)
		}
		if len(contacts) == 0 {
			failed++
			note := models.MaintenanceNotification{
				WindowID: w.ID, CustomerID: customerID,
				Status: models.MaintNotifySkipped,
				Error:  "no notify contacts with email",
			}
			ctrl.DB.Create(&note)
			continue
		}
		svcIDs := make([]uint, len(rows))
		var refs []string
		for i, r := range rows {
			svcIDs[i] = r.ServiceID
			refs = append(refs, html.EscapeString(r.ServiceRef))
		}
		body := fmt.Sprintf(
			"<p>Scheduled maintenance: <strong>%s</strong></p><p>%s</p><p>Starts %s</p><p>Affected services: %s</p>",
			html.EscapeString(w.Title),
			html.EscapeString(w.Description),
			w.StartsAt.Format(time.RFC3339),
			strings.Join(refs, ", "),
		)
		for _, ct := range contacts {
			var existing models.MaintenanceNotification
			found := ctrl.DB.Where("window_id = ? AND contact_id = ?", w.ID, ct.ID).First(&existing).Error == nil
			if found && existing.Status == models.MaintNotifySent && !req.Force {
				continue
			}
			if found && existing.Status == models.MaintNotifySent && req.Force {
				// re-send
			} else if found && existing.Status != models.MaintNotifyFailed && existing.Status != models.MaintNotifySkipped && existing.Status != models.MaintNotifyPending && !req.Force {
				continue
			}
			note := existing
			if !found {
				note = models.MaintenanceNotification{
					WindowID: w.ID, CustomerID: customerID, ContactID: &ct.ID,
				}
			}
			note.Email = ct.Email
			note.ServiceIDs = svcIDs
			if err := mail.Send(smtp, from, ct.Email, "Maintenance: "+w.Title, body); err != nil {
				note.Status = models.MaintNotifyFailed
				note.Error = err.Error()
				failed++
			} else {
				now := time.Now()
				note.SentAt = &now
				note.Status = models.MaintNotifySent
				note.Error = ""
				sent++
			}
			if note.ID == 0 {
				ctrl.DB.Create(&note)
			} else {
				ctrl.DB.Save(&note)
			}
		}
	}
	if sent > 0 && w.Status == models.MaintPlanned {
		w.Status = models.MaintNotified
		ctrl.DB.Save(&w)
	}
	return c.JSON(http.StatusOK, map[string]any{"sent": sent, "failed": failed})
}
