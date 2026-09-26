package netbox

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

// netboxSyncLockKey is the Postgres advisory-lock key shared by full sync
// and delta sync. Webhook delta runs in factum2-web; scheduled sync runs
// in factum2-netbox. The in-process mutex covers one process, the
// advisory lock covers both.
const netboxSyncLockKey int64 = 8742001

var netboxSyncMu sync.Mutex

func withNetboxSyncLock(db *gorm.DB, fn func() error) error {
	netboxSyncMu.Lock()
	defer netboxSyncMu.Unlock()
	if db.Dialector.Name() != "postgres" {
		return fn()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", netboxSyncLockKey); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", netboxSyncLockKey)
	return fn()
}
