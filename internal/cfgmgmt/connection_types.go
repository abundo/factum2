package cfgmgmt

import (
	"fmt"
	"strings"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func ServiceTypeDTO(st *models.ServiceType) models.ServiceTypeDTO {
	if st == nil {
		return models.ServiceTypeDTO{}
	}
	cts := make([]models.ServiceConnectionTypeDTO, len(st.ConnectionTypes))
	for i := range st.ConnectionTypes {
		cts[i] = ConnectionTypeDTO(&st.ConnectionTypes[i])
	}
	schema := st.Schema
	if schema == nil {
		schema = []models.FieldSchema{}
	}
	return models.ServiceTypeDTO{
		ID:              st.ID,
		Name:            st.Name,
		Description:     st.Description,
		Schema:          schema,
		Interfaces:      st.Interfaces,
		SyncSource:      st.SyncSource,
		NetboxType:      st.NetboxType,
		ConnectionTypes: cts,
	}
}

func ConnectionTypeDTO(ct *models.ServiceConnectionType) models.ServiceConnectionTypeDTO {
	if ct == nil {
		return models.ServiceConnectionTypeDTO{}
	}
	d := models.ServiceConnectionTypeDTO{
		ID:          ct.ID,
		Name:        ct.Name,
		SortOrder:   ct.SortOrder,
		ContentType: ct.ContentType,
		HasImage:    len(ct.Image) > 0,
	}
	if d.HasImage {
		d.ImageURL = fmt.Sprintf("/api/config/service-types/%d/connection-types/%d/image", ct.ServiceTypeID, ct.ID)
	}
	return d
}

func LoadServiceType(db *gorm.DB, id uint) (*models.ServiceType, error) {
	var row models.ServiceType
	err := db.Preload("ConnectionTypes", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("sort_order, id")
	}).First(&row, id).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ReplaceConnectionTypes applies DELETE omitted → UPDATE remaining → INSERT
// nameless DTOs so unique (service_type_id, name) cannot fire mid-replace.
// Remaining rows keep image/content_type. Omitted ids still referenced by
// services.connection_type_id return 409.
func ReplaceConnectionTypes(tx *gorm.DB, typeID uint, dtos []models.ServiceConnectionTypeDTO) error {
	for i := range dtos {
		dtos[i].Name = strings.TrimSpace(dtos[i].Name)
	}
	if err := validateConnectionTypeDTOs(dtos); err != nil {
		return err
	}
	var existing []models.ServiceConnectionType
	if err := tx.Where("service_type_id = ?", typeID).Find(&existing).Error; err != nil {
		return err
	}
	byID := make(map[uint]models.ServiceConnectionType, len(existing))
	for _, row := range existing {
		byID[row.ID] = row
	}
	keep := make(map[uint]bool, len(dtos))
	for _, d := range dtos {
		if d.ID != 0 {
			keep[d.ID] = true
		}
	}
	var omitIDs []uint
	for id := range byID {
		if !keep[id] {
			omitIDs = append(omitIDs, id)
		}
	}
	if len(omitIDs) > 0 {
		var n int64
		if err := tx.Model(&models.Service{}).Where("connection_type_id IN ?", omitIDs).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrConnectionTypeInUse
		}
		if err := tx.Where("id IN ?", omitIDs).Delete(&models.ServiceConnectionType{}).Error; err != nil {
			return err
		}
	}
	// Temp names so two remaining rows can swap names under SQLite's
	// immediate unique check (Postgres unique is DEFERRABLE).
	for i, d := range dtos {
		if d.ID == 0 {
			continue
		}
		row, ok := byID[d.ID]
		if !ok || row.ServiceTypeID != typeID {
			return ErrConnectionTypeNotFound
		}
		if err := tx.Model(&models.ServiceConnectionType{}).Where("id = ?", d.ID).
			Updates(map[string]any{"name": fmt.Sprintf("__ct_tmp_%d", d.ID), "sort_order": i}).Error; err != nil {
			return err
		}
	}
	for i, d := range dtos {
		if d.ID == 0 {
			continue
		}
		if err := tx.Model(&models.ServiceConnectionType{}).Where("id = ?", d.ID).
			Updates(map[string]any{"name": d.Name, "sort_order": i}).Error; err != nil {
			return uniqueOr(err)
		}
	}
	for i, d := range dtos {
		if d.ID != 0 {
			continue
		}
		row := models.ServiceConnectionType{
			ServiceTypeID: typeID,
			Name:          d.Name,
			SortOrder:     i,
		}
		if err := tx.Create(&row).Error; err != nil {
			return uniqueOr(err)
		}
	}
	return nil
}

func validateConnectionTypeDTOs(dtos []models.ServiceConnectionTypeDTO) error {
	seenName := map[string]bool{}
	seenID := map[uint]bool{}
	for _, d := range dtos {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return statusErr(400, "connection type name is required")
		}
		if len(name) > 64 {
			return statusErr(400, "connection type name is too long")
		}
		if seenName[name] {
			return ErrConnectionTypeNameTaken
		}
		seenName[name] = true
		if d.ID != 0 {
			if seenID[d.ID] {
				return statusErr(400, "duplicate connection type id")
			}
			seenID[d.ID] = true
		}
	}
	return nil
}

func uniqueOr(err error) error {
	if IsUniqueViolation(err) {
		return ErrConnectionTypeNameTaken
	}
	return err
}
