package netbox

import (
	"log/slog"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

type interfaceTypeChoiceSource interface {
	GetInterfaceTypeChoices() ([]netboxtool.InterfaceTypeChoice, error)
}

// syncInterfaceTypes upserts NetBox Interface.type choices into the Factum
// catalog. Local (source=factum) rows whose value is not in NetBox are kept.
// NetBox-sourced rows missing from OPTIONS are deleted when unused.
// An empty or failed fetch is a no-op so a parse/permission glitch cannot
// wipe the catalog.
func syncInterfaceTypes(db *gorm.DB, src interfaceTypeChoiceSource, reporter jobevent.Reporter) error {
	if src == nil {
		return nil
	}
	choices, err := src.GetInterfaceTypeChoices()
	if err != nil {
		if reporter != nil {
			reporter.Emit(jobevent.Warning, "NetBox interface types unavailable: %s", err)
		} else {
			slog.Warn("netbox: interface types", "err", err)
		}
		return nil
	}
	if len(choices) == 0 {
		if reporter != nil {
			reporter.Emit(jobevent.Warning, "NetBox interface types: empty choice list, leaving catalog unchanged")
		}
		return nil
	}
	n, err := upsertInterfaceTypes(db, choices)
	if err != nil {
		return err
	}
	if reporter != nil {
		reporter.Emit(jobevent.Info, "Netbox sync: %d interface types", n)
	}
	return nil
}

func upsertInterfaceTypes(db *gorm.DB, choices []netboxtool.InterfaceTypeChoice) (int, error) {
	var existing []models.InterfaceType
	if err := db.Find(&existing).Error; err != nil {
		return 0, err
	}
	byValue := make(map[string]models.InterfaceType, len(existing))
	for _, row := range existing {
		byValue[row.Value] = row
	}

	seen := make(map[string]bool, len(choices))
	for i, ch := range choices {
		value := ch.Value
		if value == "" {
			continue
		}
		seen[value] = true
		row, ok := byValue[value]
		if !ok {
			row = models.InterfaceType{Value: value, Source: "netbox"}
		}
		row.Value = value
		row.Label = ch.Label
		if row.Label == "" {
			row.Label = value
		}
		row.SortOrder = i
		row.Source = "netbox"
		if err := db.Save(&row).Error; err != nil {
			return 0, err
		}
		byValue[value] = row
	}

	for _, row := range existing {
		if row.Source != "netbox" || seen[row.Value] {
			continue
		}
		inUse, err := interfaceTypeInUse(db, row.Value)
		if err != nil {
			return 0, err
		}
		if inUse {
			continue
		}
		if err := db.Delete(&row).Error; err != nil {
			return 0, err
		}
	}
	return len(seen), nil
}

func interfaceTypeInUse(db *gorm.DB, value string) (bool, error) {
	var n int64
	if err := db.Model(&models.Interface{}).Where("type = ?", value).Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := db.Model(&models.InterfaceTemplate{}).Where("type = ?", value).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
