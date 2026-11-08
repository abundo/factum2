package netbox

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// nbBrief is a nested NetBox object (id, name) or a bare primary key.
// nbRouteTargetRef already accepts both shapes.
type nbBrief = nbRouteTargetRef

func briefName(b *nbBrief) string {
	if b == nil {
		return ""
	}
	return strings.TrimSpace(b.Name)
}

func joinBriefNames(items []nbBrief) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

type nbASNRangeREST struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	RIR         *nbBrief `json:"rir"`
	Start       int64    `json:"start"`
	End         int64    `json:"end"`
	Tenant      *nbBrief `json:"tenant"`
	Description string   `json:"description"`
}

type nbASNREST struct {
	ID          uint      `json:"id"`
	ASN         int64     `json:"asn"`
	RIR         *nbBrief  `json:"rir"`
	Role        *nbBrief  `json:"role"`
	Tenant      *nbBrief  `json:"tenant"`
	Description string    `json:"description"`
	Sites       []nbBrief `json:"sites"`
}

type nbRouteTargetREST struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Tenant      *nbBrief `json:"tenant"`
	Description string   `json:"description"`
}

// deleteUnseenNetbox removes source=netbox rows whose netbox id was not in
// this payload. An empty payload is a no-op so a failed or empty fetch
// does not wipe the mirror. model is a pointer to the row type.
func deleteUnseenNetbox(db *gorm.DB, model any, seen map[uint]bool, havePayload bool) (int, error) {
	if !havePayload {
		return 0, nil
	}
	var rows []struct {
		ID       uint
		NetboxID uint
	}
	if err := db.Model(model).
		Select("id", "netbox_id").
		Where("source = ? AND netbox_id != 0", models.VRFSourceNetbox).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	deleted := 0
	for _, row := range rows {
		if seen[row.NetboxID] {
			continue
		}
		if err := db.Where("id = ?", row.ID).Delete(model).Error; err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// syncASNRanges mirrors NetBox ipam.ASNRange rows. Factum-local rows stay.
func syncASNRanges(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbASNRangeREST](nb, "/api/ipam/asn-ranges/")
	if err != nil {
		return err
	}
	seen := make(map[uint]bool, len(rows))
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		created, updated, skipped, err := upsertASNRange(db, row, reporter)
		if err != nil {
			return err
		}
		if row.ID != 0 && skipped == 0 {
			seen[row.ID] = true
		}
		countNew += created
		countUpdated += updated
		countSkipped += skipped
	}
	countDeleted, err := deleteUnseenNetbox(db, &models.IpamASNRange{}, seen, len(rows) > 0)
	if err != nil {
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox ASN range sync: %d new, %d updated, %d deleted, %d skipped",
		countNew, countUpdated, countDeleted, countSkipped)
	return nil
}

func upsertASNRange(db *gorm.DB, row nbASNRangeREST, reporter jobevent.Reporter) (created, updated, skipped int, err error) {
	name := strings.TrimSpace(row.Name)
	if row.ID == 0 || name == "" || row.Start < 1 || row.End < row.Start {
		reporter.Emit(jobevent.Warning, "Netbox ASN range sync: skipping %q (netbox_id=%d, %d-%d)", name, row.ID, row.Start, row.End)
		return 0, 0, 1, nil
	}
	slug := strings.TrimSpace(row.Slug)
	rir := briefName(row.RIR)
	tenant := briefName(row.Tenant)
	desc := strings.TrimSpace(row.Description)

	var existing models.IpamASNRange
	lookupErr := db.Where("netbox_id = ?", row.ID).First(&existing).Error
	if lookupErr == nil {
		if existing.Name == name && existing.Slug == slug && existing.RIR == rir &&
			existing.Start == row.Start && existing.End == row.End && existing.Tenant == tenant &&
			existing.Description == desc && existing.Source == models.VRFSourceNetbox {
			return 0, 0, 0, nil
		}
		err := db.Model(&existing).Updates(map[string]any{
			"name":        name,
			"slug":        slug,
			"rir":         rir,
			"start":       row.Start,
			"end":         row.End,
			"tenant":      tenant,
			"description": desc,
			"source":      models.VRFSourceNetbox,
		}).Error
		if err != nil {
			return 0, 0, 0, err
		}
		return 0, 1, 0, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, lookupErr
	}
	createdRow := models.IpamASNRange{
		Name:        name,
		Slug:        slug,
		RIR:         rir,
		Start:       row.Start,
		End:         row.End,
		Tenant:      tenant,
		Description: desc,
		Source:      models.VRFSourceNetbox,
		NetboxID:    row.ID,
	}
	if err := db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	return 1, 0, 0, nil
}

// syncASNs mirrors NetBox ipam.ASN rows. Factum-local rows stay.
func syncASNs(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbASNREST](nb, "/api/ipam/asns/")
	if err != nil {
		return err
	}
	seen := make(map[uint]bool, len(rows))
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		created, updated, skipped, err := upsertASN(db, row, reporter)
		if err != nil {
			return err
		}
		if row.ID != 0 && skipped == 0 {
			seen[row.ID] = true
		}
		countNew += created
		countUpdated += updated
		countSkipped += skipped
	}
	countDeleted, err := deleteUnseenNetbox(db, &models.IpamASN{}, seen, len(rows) > 0)
	if err != nil {
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox ASN sync: %d new, %d updated, %d deleted, %d skipped",
		countNew, countUpdated, countDeleted, countSkipped)
	return nil
}

func upsertASN(db *gorm.DB, row nbASNREST, reporter jobevent.Reporter) (created, updated, skipped int, err error) {
	if row.ID == 0 || row.ASN < 1 {
		reporter.Emit(jobevent.Warning, "Netbox ASN sync: skipping asn=%d (netbox_id=%d)", row.ASN, row.ID)
		return 0, 0, 1, nil
	}
	rir := briefName(row.RIR)
	role := briefName(row.Role)
	tenant := briefName(row.Tenant)
	desc := strings.TrimSpace(row.Description)
	sites := joinBriefNames(row.Sites)

	var existing models.IpamASN
	lookupErr := db.Where("netbox_id = ?", row.ID).First(&existing).Error
	if lookupErr == nil {
		if existing.ASN == row.ASN && existing.RIR == rir && existing.Role == role && existing.Tenant == tenant &&
			existing.Description == desc && existing.Sites == sites && existing.Source == models.VRFSourceNetbox {
			return 0, 0, 0, nil
		}
		err := db.Model(&existing).Updates(map[string]any{
			"asn":         row.ASN,
			"rir":         rir,
			"role":        role,
			"tenant":      tenant,
			"description": desc,
			"sites":       sites,
			"source":      models.VRFSourceNetbox,
		}).Error
		if err != nil {
			return 0, 0, 0, err
		}
		return 0, 1, 0, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, lookupErr
	}
	createdRow := models.IpamASN{
		ASN:         row.ASN,
		RIR:         rir,
		Role:        role,
		Tenant:      tenant,
		Description: desc,
		Sites:       sites,
		Source:      models.VRFSourceNetbox,
		NetboxID:    row.ID,
	}
	if err := db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	return 1, 0, 0, nil
}

// syncRouteTargets mirrors NetBox ipam.RouteTarget rows. VRF import and
// export lists stay as names on the VRF; this is the catalog itself.
func syncRouteTargets(db *gorm.DB, nb *netboxtool.NetboxClient, reporter jobevent.Reporter) error {
	rows, err := restListAll[nbRouteTargetREST](nb, "/api/ipam/route-targets/")
	if err != nil {
		return err
	}
	seen := make(map[uint]bool, len(rows))
	var countNew, countUpdated, countSkipped int
	for _, row := range rows {
		created, updated, skipped, err := upsertRouteTarget(db, row, reporter)
		if err != nil {
			return err
		}
		if row.ID != 0 && skipped == 0 {
			seen[row.ID] = true
		}
		countNew += created
		countUpdated += updated
		countSkipped += skipped
	}
	countDeleted, err := deleteUnseenNetbox(db, &models.IpamRouteTarget{}, seen, len(rows) > 0)
	if err != nil {
		return err
	}
	reporter.Emit(jobevent.Info, "Netbox route target sync: %d new, %d updated, %d deleted, %d skipped",
		countNew, countUpdated, countDeleted, countSkipped)
	return nil
}

func upsertRouteTarget(db *gorm.DB, row nbRouteTargetREST, reporter jobevent.Reporter) (created, updated, skipped int, err error) {
	name := strings.TrimSpace(row.Name)
	if row.ID == 0 || name == "" {
		reporter.Emit(jobevent.Warning, "Netbox route target sync: skipping unnamed route target (netbox_id=%d)", row.ID)
		return 0, 0, 1, nil
	}
	tenant := briefName(row.Tenant)
	desc := strings.TrimSpace(row.Description)

	var existing models.IpamRouteTarget
	lookupErr := db.Where("netbox_id = ?", row.ID).First(&existing).Error
	if lookupErr == nil {
		if existing.Name == name && existing.Tenant == tenant && existing.Description == desc && existing.Source == models.VRFSourceNetbox {
			return 0, 0, 0, nil
		}
		err := db.Model(&existing).Updates(map[string]any{
			"name":        name,
			"tenant":      tenant,
			"description": desc,
			"source":      models.VRFSourceNetbox,
		}).Error
		if err != nil {
			return 0, 0, 0, err
		}
		return 0, 1, 0, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, 0, 0, lookupErr
	}
	createdRow := models.IpamRouteTarget{
		Name:        name,
		Tenant:      tenant,
		Description: desc,
		Source:      models.VRFSourceNetbox,
		NetboxID:    row.ID,
	}
	if err := db.Create(&createdRow).Error; err != nil {
		return 0, 0, 0, err
	}
	return 1, 0, 0, nil
}
