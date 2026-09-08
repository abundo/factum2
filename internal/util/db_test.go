package util

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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

func postgresTestConfig(t *testing.T) *ConfigDB {
	t.Helper()
	host := os.Getenv("FACTUM2_TEST_PG_HOST")
	user := os.Getenv("FACTUM2_TEST_PG_USER")
	pass := os.Getenv("FACTUM2_TEST_PG_PASS")
	database := os.Getenv("FACTUM2_TEST_PG_DATABASE")
	if host == "" || user == "" || pass == "" || database == "" {
		t.Skip("set FACTUM2_TEST_PG_HOST, FACTUM2_TEST_PG_USER, FACTUM2_TEST_PG_PASS, FACTUM2_TEST_PG_DATABASE to run")
	}
	return &ConfigDB{
		Host:     host,
		Port:     os.Getenv("FACTUM2_TEST_PG_PORT"),
		User:     user,
		Pass:     pass,
		Database: database,
	}
}

func openPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := ConnectDatabase(postgresTestConfig(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db.Logger = logger.Default.LogMode(logger.Silent)
	return db
}

func TestMigrateDatabaseEmptyPostgres(t *testing.T) {
	db := openPostgresTestDB(t)
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("first migrate on empty postgres: %v", err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("second migrate (already applied): %v", err)
	}
}

func TestMigrateDatabaseAdoptsExistingPostgres(t *testing.T) {
	db := openPostgresTestDB(t)
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("setup migrate: %v", err)
	}
	if err := db.Exec(`DROP TABLE goose_db_version`).Error; err != nil {
		t.Fatalf("drop goose table: %v", err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("adopt migrate: %v", err)
	}
	if !db.Migrator().HasTable("goose_db_version") {
		t.Fatal("expected goose_db_version after adopt")
	}
	var version int64
	if err := db.Raw(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&version).Error; err != nil {
		t.Fatalf("version: %v", err)
	}
	if version < 1 {
		t.Fatalf("stamped version = %d, want >= 1", version)
	}
}

func TestMigrateDatabaseAddsDNSZoneEditorOnAdoptedSchema(t *testing.T) {
	db := openPostgresTestDB(t)
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("setup migrate: %v", err)
	}

	// Production DBs created before the DNS zone editor, then stamped at
	// goose v1, have neither the settings columns nor the dns_* tables.
	if err := db.Exec(`
		ALTER TABLE settings DROP COLUMN IF EXISTS dns_zones_enabled;
		ALTER TABLE settings DROP COLUMN IF EXISTS dhcp_enabled;
		ALTER TABLE settings DROP COLUMN IF EXISTS dns_db_file;
		ALTER TABLE IF EXISTS ipam_prefixes DROP COLUMN IF EXISTS dhcp_enabled;
		DROP TABLE IF EXISTS dns_zone_records, dns_zones, dns_template_nameservers, dns_templates, dns_soa_templates, dns_dnssec_policies CASCADE;
		DELETE FROM goose_db_version WHERE version_id > 1;
	`).Error; err != nil {
		t.Fatalf("strip dns zone editor schema: %v", err)
	}

	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("migrate adopted schema: %v", err)
	}
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("second migrate (idempotent): %v", err)
	}

	var hasCol bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'settings'
			  AND column_name = 'dns_zones_enabled'
		)`).Scan(&hasCol).Error; err != nil {
		t.Fatalf("column check: %v", err)
	}
	if !hasCol {
		t.Fatal("expected settings.dns_zones_enabled after migrate")
	}
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'settings'
			  AND column_name = 'dns_db_file'
		)`).Scan(&hasCol).Error; err != nil {
		t.Fatalf("dns_db_file column check: %v", err)
	}
	if !hasCol {
		t.Fatal("expected settings.dns_db_file after migrate")
	}
	if !db.Migrator().HasTable("dns_zones") {
		t.Fatal("expected dns_zones after migrate")
	}
	var version int64
	if err := db.Raw(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&version).Error; err != nil {
		t.Fatalf("version: %v", err)
	}
	if version < 3 {
		t.Fatalf("stamped version = %d, want >= 3", version)
	}
}

func TestMigrateDatabaseAddsDnsDbFileAfterV2(t *testing.T) {
	db := openPostgresTestDB(t)
	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("setup migrate: %v", err)
	}

	// v1.0.8 applied 00002 without dns_db_file. Adopted DBs are at goose
	// version 2 and still lack the column; loading Settings then fails.
	if err := db.Exec(`
		ALTER TABLE settings DROP COLUMN IF EXISTS dns_db_file;
		DELETE FROM goose_db_version WHERE version_id > 2;
	`).Error; err != nil {
		t.Fatalf("strip dns_db_file: %v", err)
	}

	if err := MigrateDatabase(db); err != nil {
		t.Fatalf("migrate after v2: %v", err)
	}

	var hasCol bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'settings'
			  AND column_name = 'dns_db_file'
		)`).Scan(&hasCol).Error; err != nil {
		t.Fatalf("column check: %v", err)
	}
	if !hasCol {
		t.Fatal("expected settings.dns_db_file after migrate")
	}
	var version int64
	if err := db.Raw(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&version).Error; err != nil {
		t.Fatalf("version: %v", err)
	}
	if version < 3 {
		t.Fatalf("stamped version = %d, want >= 3", version)
	}
}
