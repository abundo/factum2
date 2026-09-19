package dcim

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func GetLayout(db *gorm.DB, userID uint, scope string) (*LayoutDTO, error) {
	scope = strings.TrimSpace(scope)
	if userID == 0 {
		return nil, errf(http.StatusUnauthorized, ReasonForbidden, "layout is per user")
	}
	if scope == "" {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "scope is required")
	}
	var row models.ConnectionViewLayout
	err := db.Where("user_id = ? AND scope = ?", userID, scope).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &LayoutDTO{Scope: scope, Revision: 1, Nodes: map[string]XY{}}, nil
	}
	if err != nil {
		return nil, err
	}
	nodes := map[string]XY{}
	if strings.TrimSpace(row.Nodes) != "" {
		if err := json.Unmarshal([]byte(row.Nodes), &nodes); err != nil {
			nodes = map[string]XY{}
		}
	}
	return &LayoutDTO{Scope: row.Scope, Revision: row.Revision, Nodes: nodes}, nil
}

func SaveLayout(db *gorm.DB, userID uint, scope string, revision int, nodes map[string]XY) (*LayoutDTO, error) {
	scope = strings.TrimSpace(scope)
	if userID == 0 {
		return nil, errf(http.StatusUnauthorized, ReasonForbidden, "layout is per user")
	}
	if scope == "" {
		return nil, errf(http.StatusBadRequest, ReasonInvalid, "scope is required")
	}
	if nodes == nil {
		nodes = map[string]XY{}
	}
	clean := map[string]XY{}
	for k, v := range nodes {
		if k == "" {
			continue
		}
		if !finiteFloat(v.X) || !finiteFloat(v.Y) {
			return nil, errf(http.StatusBadRequest, ReasonInvalid, "node coordinates must be finite")
		}
		clean[k] = v
	}
	body, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var row models.ConnectionViewLayout
		err := tx.Where("user_id = ? AND scope = ?", userID, scope).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = models.ConnectionViewLayout{UserID: userID, Scope: scope, Revision: 1, Nodes: string(body)}
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		if revision != 0 && row.Revision != revision {
			return errf(http.StatusConflict, ReasonStaleVersion, "layout was changed by another editor")
		}
		row.Nodes = string(body)
		row.Revision++
		return tx.Save(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return GetLayout(db, userID, scope)
}

func finiteFloat(v float64) bool {
	return !((v != v) || v > 1e15 || v < -1e15)
}
