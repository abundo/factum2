package util

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMigrateDatabaseEmptySQLite is the same first-boot path every other
// package's newTestDB already exercises (SQLite, so dedupeInterfaces is a
// no-op). Kept here so a change to MigrateDatabase is caught in this package
// without depending on those callers.
func TestMigrateDatabaseEmptySQLite(t *testing.T) {
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

	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("first migrate on empty db: %v", err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("second migrate (already applied): %v", err)
	}
}

type leftoverPackRow struct {
	ID              uint `gorm:"primaryKey"`
	ServiceTypeID   uint
	Platform        string
	PayloadKind     string
	ApplyTemplate   string
	CleanupTemplate string
	SeedChecksum    string
}

func (leftoverPackRow) TableName() string { return "platform_packs" }

func openMigrateTestDB(t *testing.T) *gorm.DB {
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
	return db
}

func TestMigrateDatabaseRefusesUnmigratedPack(t *testing.T) {
	db := openMigrateTestDB(t)

	if err := db.AutoMigrate(&leftoverPackRow{}); err != nil {
		t.Fatal(err)
	}
	// No matching service type, so Seed cannot copy this pack onto a CLI object.
	if err := db.Create(&leftoverPackRow{ServiceTypeID: 999, Platform: "custom-nos"}).Error; err != nil {
		t.Fatal(err)
	}
	err := MigrateDatabase(db)
	if err == nil {
		t.Fatal("expected migrate to refuse drop of unmigrated pack")
	}
	if !strings.Contains(err.Error(), "cannot drop platform_packs") {
		t.Fatalf("err = %v", err)
	}
	if !db.Migrator().HasTable("platform_packs") {
		t.Fatal("platform_packs was dropped despite unmigrated pack")
	}
}

func TestMigrateDatabaseMigratesLeftoverELINEPacks(t *testing.T) {
	db := openMigrateTestDB(t)

	if err := db.AutoMigrate(&leftoverPackRow{}); err != nil {
		t.Fatal(err)
	}
	for _, plat := range []string{"eos", "ios-xr", "sros", "sros-md"} {
		if err := db.Create(&leftoverPackRow{ServiceTypeID: 1, Platform: plat}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("migrate leftover ELINE packs: %v", err)
	}
	if db.Migrator().HasTable("platform_packs") {
		t.Fatal("platform_packs still present after leftover ELINE packs migrated")
	}
}

type leftoverTemplateRow struct {
	ID          uint `gorm:"primaryKey"`
	Name        string
	Platform    string
	PayloadKind string
	Body        string
	ScopeID     *uint
	Enabled     bool
}

func (leftoverTemplateRow) TableName() string { return "config_templates" }

func TestMigrateDatabaseRefusesUnmigratedTemplate(t *testing.T) {
	db := openMigrateTestDB(t)

	if err := db.AutoMigrate(&leftoverTemplateRow{}); err != nil {
		t.Fatal(err)
	}
	missingParent := uint(999)
	if err := db.Create(&leftoverTemplateRow{Name: "banner", Platform: "eos", ScopeID: &missingParent, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	err := MigrateDatabase(db)
	if err == nil {
		t.Fatal("expected migrate to refuse drop of unmigrated template")
	}
	if !strings.Contains(err.Error(), "cannot drop config_templates") {
		t.Fatalf("err = %v", err)
	}
	if !db.Migrator().HasTable("config_templates") {
		t.Fatal("config_templates was dropped despite unmigrated template")
	}
}

func TestMigrateDatabaseMigratesLeftoverTemplate(t *testing.T) {
	db := openMigrateTestDB(t)

	if err := db.AutoMigrate(&leftoverTemplateRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&leftoverTemplateRow{Name: "banner", Platform: "eos", Body: "banner motd x", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("migrate leftover template: %v", err)
	}
	if db.Migrator().HasTable("config_templates") {
		t.Fatal("config_templates still present after leftover template migrated")
	}
}

// TestMigrateDatabaseEmptyPostgres is the production first-boot path that
// used to fail with SQLSTATE 42P01 ("relation interfaces does not exist")
// because dedupeInterfaces queried the table before AutoMigrate created it.
// Skipped unless FACTUM2_TEST_PG_* is set - CI and local sqlite runs
// don't have a throwaway Postgres.
func TestMigrateDatabaseEmptyPostgres(t *testing.T) {
	host := os.Getenv("FACTUM2_TEST_PG_HOST")
	user := os.Getenv("FACTUM2_TEST_PG_USER")
	pass := os.Getenv("FACTUM2_TEST_PG_PASS")
	database := os.Getenv("FACTUM2_TEST_PG_DATABASE")
	if host == "" || user == "" || pass == "" || database == "" {
		t.Skip("set FACTUM2_TEST_PG_HOST, FACTUM2_TEST_PG_USER, FACTUM2_TEST_PG_PASS, FACTUM2_TEST_PG_DATABASE to run")
	}
	cfg := &ConfigDB{
		Host:     host,
		Port:     os.Getenv("FACTUM2_TEST_PG_PORT"),
		User:     user,
		Pass:     pass,
		Database: database,
	}
	db, err := ConnectDatabase(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db.Logger = logger.Default.LogMode(logger.Silent)

	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("first migrate on empty postgres: %v", err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("second migrate (already applied): %v", err)
	}
}
