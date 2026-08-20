package factum

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/abundo/factum2/models"
)

// GetLibrenmsPendingDeletes is GET /api/librenms/pending-deletes.
func (factum *FactumClient) GetLibrenmsPendingDeletes() ([]*models.LibrenmsPendingDelete, error) {
	var rows []*models.LibrenmsPendingDelete
	if err := factum.doJSON(http.MethodGet, "/api/librenms/pending-deletes", nil, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*models.LibrenmsPendingDelete{}
	}
	return rows, nil
}

// UpsertLibrenmsPendingDelete is PUT /api/librenms/pending-deletes/:device_id.
func (factum *FactumClient) UpsertLibrenmsPendingDelete(row *models.LibrenmsPendingDelete) (*models.LibrenmsPendingDelete, error) {
	if row == nil {
		return nil, fmt.Errorf("pending delete is nil")
	}
	body, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var out models.LibrenmsPendingDelete
	path := "/api/librenms/pending-deletes/" + strconv.Itoa(row.DeviceID)
	if err := factum.doJSON(http.MethodPut, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLibrenmsPendingDelete is DELETE /api/librenms/pending-deletes/:device_id.
func (factum *FactumClient) DeleteLibrenmsPendingDelete(deviceID int) error {
	path := "/api/librenms/pending-deletes/" + strconv.Itoa(deviceID)
	return factum.doJSON(http.MethodDelete, path, nil, nil)
}
