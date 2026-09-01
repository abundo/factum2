package util

import (
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func settingsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.Settings{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestOrganizationEnabled(t *testing.T) {
	db := settingsTestDB(t)

	if OrganizationEnabled(db) {
		t.Fatal("missing settings row: want disabled")
	}

	if _, err := GetOrCreateSettings(db); err != nil {
		t.Fatalf("GetOrCreateSettings: %v", err)
	}
	if OrganizationEnabled(db) {
		t.Fatal("nil OrganizationEnabled: want disabled")
	}

	on := true
	if err := db.Model(&models.Settings{}).Where("id = 1").Update("organization_enabled", on).Error; err != nil {
		t.Fatalf("set true: %v", err)
	}
	if !OrganizationEnabled(db) {
		t.Fatal("true OrganizationEnabled: want enabled")
	}

	off := false
	if err := db.Model(&models.Settings{}).Where("id = 1").Update("organization_enabled", off).Error; err != nil {
		t.Fatalf("set false: %v", err)
	}
	if OrganizationEnabled(db) {
		t.Fatal("false OrganizationEnabled: want disabled")
	}
}
