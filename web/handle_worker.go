package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/abundo/factum2/internal/worker"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// WorkerStatusEntry is one configured models.WorkerNode merged with its
// live connection state from ctrl.RemoteManager - unlike ApiWorkerPing,
// this always lists every configured node (connected or not), since
// RemoteManager already holds a live-or-retrying connection to each one
// rather than needing a broadcast round trip to find out.
type WorkerStatusEntry struct {
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Enabled   bool      `json:"enabled"`
	Connected bool      `json:"connected"`
	Hostname  string    `json:"hostname"`
	Roles     []string  `json:"roles"`
	LastSeen  time.Time `json:"last_seen"`
	LastError string    `json:"last_error,omitempty"`
}

// ApiWorkerStatus lists every configured worker node with its live
// connection status - the sync status page's main table. Replaces
// ApiWorkerPing's broadcast-and-wait for nodes managed via
// ctrl.RemoteManager: status here is a plain read of already-known state.
func (ctrl *Controller) ApiWorkerStatus(c *echo.Context) error {
	var nodes []models.WorkerNode
	if err := ctrl.DB.Find(&nodes).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	statusByName := ctrl.RemoteManager.StatusAll()

	response := make([]WorkerStatusEntry, 0, len(nodes))
	for _, node := range nodes {
		status := statusByName[node.Name]
		response = append(response, WorkerStatusEntry{
			Name:      node.Name,
			Address:   node.Address,
			Enabled:   node.Enabled,
			Connected: status.Connected,
			Hostname:  status.Hostname,
			Roles:     status.Roles,
			LastSeen:  status.LastSeen,
			LastError: status.LastError,
		})
	}
	return c.JSON(http.StatusOK, response)
}

// ApiWorkerNodeToken returns a single WorkerNode's stored token. Kept
// separate from GetAll/GetOne (models.WorkerNode.Token is json:"-", so
// neither ever includes it) so a bulk list/detail fetch never carries
// secrets - this is the one explicit, single-node reveal path, used to
// prefill the admin UI's token field so its own eye icon can show it.
func (ctrl *Controller) ApiWorkerNodeToken(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var node models.WorkerNode
	if err := ctrl.DB.First(&node, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	return c.JSON(http.StatusOK, map[string]any{"token": node.Token})
}

// ApiWorkerNodeCreate/ApiWorkerNodeUpdate replace the generic
// SecureCRUDHandler for WorkerNode's write side: Token is json:"-" on the
// model (so GetAll/GetOne never leak it), but that same tag makes it
// invisible to SecureCRUDHandler's DTO<->model JSON round-trip in both
// directions - without a custom handler, a submitted token would silently
// never be written to the DB. Same reasoning as ApiUserCreate/ApiUserUpdate
// for PasswordHash (see handle_user_api.go).
func (ctrl *Controller) ApiWorkerNodeCreate(c *echo.Context) error {
	var dto models.WorkerNodeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	node := models.WorkerNode{
		Name:    dto.Name,
		Address: dto.Address,
		Token:   dto.Token,
		Enabled: dto.Enabled,
	}
	if err := ctrl.DB.Create(&node).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, node)
}

func (ctrl *Controller) ApiWorkerNodeUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	var node models.WorkerNode
	if err := ctrl.DB.First(&node, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}

	var dto models.WorkerNodeDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	node.Name = dto.Name
	node.Address = dto.Address
	node.Enabled = dto.Enabled
	// An empty token means "unchanged" (see WorkerNodeDTO.Token's doc
	// comment) - the frontend prefills the field with the current token, so
	// this only fires on an actual edit/regenerate, not a stale cleared field.
	if dto.Token != "" {
		node.Token = dto.Token
	}

	if err := ctrl.DB.Save(&node).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, node)
}

// ApiWorkerRunRequest is the body "factum2-worker run" posts - see
// internal/worker/run_client.go's RunRemote, its client-side counterpart.
type ApiWorkerRunRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// ApiWorkerRun dispatches a predefined command to every connected node
// handling the given role (ctrl.RemoteManager.RunAndWait) and streams each
// LogMsg back as one NDJSON line, flushed immediately - the "factum2-worker
// run" CLI's replacement for dialing rabbitmq directly, now that only the
// primary holds the hub connections a command can actually be dispatched
// over.
func (ctrl *Controller) ApiWorkerRun(c *echo.Context) error {
	var req ApiWorkerRunRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "command is required"})
	}

	c.Response().Header().Set(echo.HeaderContentType, "application/x-ndjson")
	flusher := http.NewResponseController(c.Response())
	var wroteAny bool
	_, err := ctrl.RemoteManager.RunAndWait(c.Request().Context(), req.Command, req.Args, func(msg worker.LogMsg) {
		line, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			return
		}
		line = append(line, '\n')
		if _, writeErr := c.Response().Write(line); writeErr != nil {
			return // client disconnected mid-stream
		}
		_ = flusher.Flush()
		wroteAny = true
	})
	if !wroteAny {
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
		}
		return c.NoContent(http.StatusOK)
	}
	// The final result is already the NDJSON StreamExit line - the HTTP
	// status was committed to 200 the moment the first line was written, so
	// there's nothing left to report at this level even if err != nil here.
	return nil
}
