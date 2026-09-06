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
	"gorm.io/gorm"
)

func (ctrl *Controller) ApiMaintenanceList(c *echo.Context) error {
	var rows []models.MaintenanceWindow
	if err := ctrl.DB.Preload("Resources").Order("starts_at desc").Find(&rows).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	for i := range rows {
		rows[i].Resources = windowResources(rows[i])
	}
	return c.JSON(http.StatusOK, rows)
}

func (ctrl *Controller) ApiMaintenanceGet(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}
	var w models.MaintenanceWindow
	if err := ctrl.DB.Preload("Resources").First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	resources := windowResources(w)
	w.Resources = resources
	impact, _ := optical.ResourcesImpact(ctrl.DB, resources)
	var notes []models.MaintenanceNotification
	_ = ctrl.DB.Where("window_id = ?", w.ID).Find(&notes).Error
	return c.JSON(http.StatusOK, map[string]any{
		"window":        w,
		"resources":     ctrl.labeledResources(resources),
		"impact":        impact,
		"notifications": notes,
	})
}

type maintenanceResourceIn struct {
	ResourceType string `json:"resource_type"`
	ResourceID   uint   `json:"resource_id"`
}

type maintenanceCreateRequest struct {
	Title        string                  `json:"title"`
	Description  string                  `json:"description"`
	ResourceType string                  `json:"resource_type"`
	ResourceID   uint                    `json:"resource_id"`
	Resources    []maintenanceResourceIn `json:"resources"`
	StartsAt     time.Time               `json:"starts_at"`
	EndsAt       time.Time               `json:"ends_at"`
	Status       string                  `json:"status"`
}

type maintenanceResourceView struct {
	ResourceType string `json:"resource_type"`
	ResourceID   uint   `json:"resource_id"`
	Label        string `json:"label"`
}

func (ctrl *Controller) ApiMaintenanceCreate(c *echo.Context) error {
	var req maintenanceCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	resources := normalizeMaintenanceResources(req)
	status := req.Status
	if status == "" {
		status = models.MaintDraft
	}
	if status != models.MaintDraft && status != models.MaintPlanned {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "create as draft or planned"})
	}
	if req.Title == "" || len(resources) == 0 || req.StartsAt.IsZero() {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "title, resource and starts_at required"})
	}
	if !req.EndsAt.IsZero() && !req.EndsAt.After(req.StartsAt) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "ends_at must be after starts_at"})
	}
	for _, r := range resources {
		if err := ctrl.maintenanceResourceExists(r.ResourceType, r.ResourceID); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
	}
	w := models.MaintenanceWindow{
		Title:        req.Title,
		Description:  req.Description,
		ResourceType: resources[0].ResourceType,
		ResourceID:   resources[0].ResourceID,
		StartsAt:     req.StartsAt,
		EndsAt:       req.EndsAt,
		Status:       status,
	}
	if user, ok := c.Get("user").(models.User); ok {
		w.CreatedBy = user.ID
	}
	err := ctrl.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&w).Error; err != nil {
			return err
		}
		rows := make([]models.MaintenanceResource, len(resources))
		for i, r := range resources {
			rows[i] = models.MaintenanceResource{
				WindowID:     w.ID,
				ResourceType: r.ResourceType,
				ResourceID:   r.ResourceID,
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		w.Resources = rows
		return nil
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, w)
}

func normalizeMaintenanceResources(req maintenanceCreateRequest) []models.MaintenanceResource {
	seen := map[string]bool{}
	var out []models.MaintenanceResource
	add := func(kind string, id uint) {
		if kind == "" || id == 0 {
			return
		}
		key := kind + ":" + fmt.Sprint(id)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, models.MaintenanceResource{ResourceType: kind, ResourceID: id})
	}
	for _, r := range req.Resources {
		add(r.ResourceType, r.ResourceID)
	}
	add(req.ResourceType, req.ResourceID)
	return out
}

func windowResources(w models.MaintenanceWindow) []models.MaintenanceResource {
	if len(w.Resources) > 0 {
		return w.Resources
	}
	if w.ResourceType != "" && w.ResourceID != 0 {
		return []models.MaintenanceResource{{
			WindowID:     w.ID,
			ResourceType: w.ResourceType,
			ResourceID:   w.ResourceID,
		}}
	}
	return nil
}

func (ctrl *Controller) labeledResources(resources []models.MaintenanceResource) []maintenanceResourceView {
	out := make([]maintenanceResourceView, 0, len(resources))
	for _, r := range resources {
		out = append(out, maintenanceResourceView{
			ResourceType: r.ResourceType,
			ResourceID:   r.ResourceID,
			Label:        ctrl.resourceLabel(r.ResourceType, r.ResourceID),
		})
	}
	return out
}

func (ctrl *Controller) resourceLabel(kind string, id uint) string {
	switch kind {
	case models.MaintResourceDevice:
		var d models.Device
		if ctrl.DB.Select("name").First(&d, id).Error == nil && d.Name != "" {
			return d.Name
		}
	case models.MaintResourceConnection:
		if label := ctrl.connectionLabel(id); label != "" {
			return label
		}
	case models.MaintResourceInterface:
		var iface models.Interface
		if ctrl.DB.Select("name").First(&iface, id).Error == nil && iface.Name != "" {
			return iface.Name
		}
	case models.MaintResourceWavelength, models.MaintResourceFiber:
		var s models.Service
		if ctrl.DB.Select("service_id").First(&s, id).Error == nil && s.ServiceID != "" {
			return s.ServiceID
		}
	}
	return fmt.Sprintf("%s #%d", kind, id)
}

func (ctrl *Controller) connectionLabel(id uint) string {
	var conn models.Connection
	if ctrl.DB.First(&conn, id).Error != nil {
		return ""
	}
	var a, b models.Device
	var ia, ib models.Interface
	_ = ctrl.DB.Select("name").First(&a, conn.DeviceAID)
	_ = ctrl.DB.Select("name").First(&b, conn.DeviceBID)
	_ = ctrl.DB.Select("name").First(&ia, conn.InterfaceAID)
	_ = ctrl.DB.Select("name").First(&ib, conn.InterfaceBID)
	return connectionDisplayName(a.Name, ia.Name, b.Name, ib.Name, conn.Label)
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
	case models.MaintResourceWavelength, models.MaintResourceFiber:
		ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Count(&n)
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
	if err := ctrl.DB.Preload("Resources").First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	impact, err := optical.ResourcesImpact(ctrl.DB, windowResources(w))
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
	if err := ctrl.DB.Preload("Resources").First(&w, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if w.Status != models.MaintPlanned && w.Status != models.MaintNotified {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "notify only from planned or notified"})
	}
	var req maintenanceNotifyRequest
	_ = c.Bind(&req)

	impact, err := optical.ResourcesImpact(ctrl.DB, windowResources(w))
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
