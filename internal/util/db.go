package util

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/dbmigrate"
	"github.com/abundo/factum2/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ConnectDatabase opens Postgres and does not apply schema migrations.
// Call this from the web server and CLIs. Schema changes belong in the
// dedicated `migrate` command (cmdbase.Migrate → MigrateDatabase); running
// migrations as a side effect of start/sync/createadmin rewrites tables
// while factum2-web may already be serving.
func ConnectDatabase(config *ConfigDB) (*gorm.DB, error) {
	port := config.Port
	if port == "" {
		port = "5432"
	}
	dns := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s timezone=Europe/Stockholm",
		config.Host, port, config.User, config.Pass, config.Database)
	slog.Debug("database", "open", dns)
	db, err := gorm.Open(postgres.Open(dns), &gorm.Config{
		// cfgmgmt.Seed uses First() then Create(); GORM otherwise logs every
		// miss as "record not found" during migrate on an empty database.
		Logger: logger.New(log.New(os.Stdout, "\n", log.LstdFlags), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		}),
	})
	if err != nil {
		return nil, err
	}

	// database/sql defaults to an unlimited connection pool - every caller
	// of ConnectDatabase (web server, CLI tools, and formerly one per
	// Netbox webhook, see internal/netbox.SyncDB) is otherwise free to open
	// as many Postgres backends as it has concurrent goroutines, which is
	// what exhausted Postgres's max_connections (SQLSTATE 53300) under a
	// burst of Netbox webhooks. Cap it well under a typical max_connections
	// so callers block/queue instead of opening new backends once the pool
	// is full.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}

// MigrateDatabase applies schema migrations then cfgmgmt.Seed. Only
// cmdbase.Migrate (and tests) should call this — not ConnectDatabase.
//
// Postgres: goose SQL in internal/dbmigrate (existing AutoMigrate DBs are
// stamped at the baseline version). SQLite: GORM AutoMigrateAll, for unit
// tests only.
func MigrateDatabase(db *gorm.DB) error {
	if db.Dialector.Name() == "postgres" {
		if err := dbmigrate.Up(db); err != nil {
			return err
		}
	} else {
		if err := models.AutoMigrateAll(db); err != nil {
			return err
		}
	}

	if err := cfgmgmt.Seed(db); err != nil {
		return err
	}
	if err := cfgmgmt.AssertPacksHaveCLITwins(db); err != nil {
		return err
	}
	if err := cfgmgmt.AssertTemplatesHaveCLITwins(db); err != nil {
		return err
	}
	return cfgmgmt.DropPackAndTemplateTables(db)
}
