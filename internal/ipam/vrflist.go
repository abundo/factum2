package ipam

import (
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// VRFListItem is a VRF plus its namespace name for the address picker.
type VRFListItem struct {
	ID            uint   `json:"id"`
	NamespaceID   uint   `json:"namespace_id"`
	NamespaceName string `json:"namespace_name"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	IsDefault     bool   `json:"is_default"`
	PrefixCount   int64  `json:"prefix_count"`
}

func ListAllVRFs(db *gorm.DB) ([]VRFListItem, error) {
	var rows []models.IpamVRF
	if err := db.Order("name, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	nsIDs := make([]uint, 0)
	seenNS := map[uint]bool{}
	for _, r := range rows {
		if seenNS[r.NamespaceID] {
			continue
		}
		seenNS[r.NamespaceID] = true
		nsIDs = append(nsIDs, r.NamespaceID)
	}
	nsName := map[uint]string{}
	if len(nsIDs) > 0 {
		var nss []models.IpamNamespace
		if err := db.Where("id IN ?", nsIDs).Find(&nss).Error; err != nil {
			return nil, err
		}
		for _, ns := range nss {
			nsName[ns.ID] = ns.Name
		}
	}
	counts, err := prefixCountAllVRFs(db)
	if err != nil {
		return nil, err
	}
	out := make([]VRFListItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, VRFListItem{
			ID:            r.ID,
			NamespaceID:   r.NamespaceID,
			NamespaceName: nsName[r.NamespaceID],
			Name:          r.Name,
			Description:   r.Description,
			IsDefault:     r.IsDefault,
			PrefixCount:   counts[r.ID],
		})
	}
	return out, nil
}

func prefixCountAllVRFs(db *gorm.DB) (map[uint]int64, error) {
	type row struct {
		VRFID uint  `gorm:"column:vrf_id"`
		N     int64 `gorm:"column:n"`
	}
	var rows []row
	if err := db.Model(&models.IpamPrefix{}).
		Select("vrf_id, count(*) as n").
		Group("vrf_id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint]int64{}
	for _, r := range rows {
		out[r.VRFID] = r.N
	}
	return out, nil
}
