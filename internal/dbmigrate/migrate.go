// Package dbmigrate applies versioned Postgres schema migrations (goose SQL
// under sql/). Production schema is those files, not GORM AutoMigrate.
// Existing databases created by AutoMigrate are stamped at version 1
// (the baseline dump) without re-running CREATE TABLE.
package dbmigrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFS embed.FS

func Up(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := adoptIfNeeded(ctx, sqlDB); err != nil {
		return err
	}
	fsys, err := fs.Sub(migrationFS, "sql")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// adoptIfNeeded stamps goose version 1 when the database already has factum
// tables (created by AutoMigrate) but no goose history. Fresh databases are
// left alone so goose applies 00001_baseline.sql.
func adoptIfNeeded(ctx context.Context, sqlDB *sql.DB) error {
	hasGoose, err := tableExists(ctx, sqlDB, goose.DefaultTablename)
	if err != nil {
		return err
	}
	if hasGoose {
		return nil
	}
	hasUsers, err := tableExists(ctx, sqlDB, "users")
	if err != nil {
		return err
	}
	if !hasUsers {
		return nil
	}
	store, err := database.NewStore(database.DialectPostgres, goose.DefaultTablename)
	if err != nil {
		return err
	}
	if err := store.CreateVersionTable(ctx, sqlDB); err != nil {
		return fmt.Errorf("adopt existing schema: create version table: %w", err)
	}
	if err := store.Insert(ctx, sqlDB, database.InsertRequest{Version: 0}); err != nil {
		return fmt.Errorf("adopt existing schema: stamp 0: %w", err)
	}
	if err := store.Insert(ctx, sqlDB, database.InsertRequest{Version: 1}); err != nil {
		return fmt.Errorf("adopt existing schema: stamp 1: %w", err)
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, name).Scan(&exists)
	return exists, err
}
