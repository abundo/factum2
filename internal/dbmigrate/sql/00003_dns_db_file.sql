-- settings.dns_db_file (v1.0.8).
--
-- 00001_baseline.sql already has this column, so fresh installs no-op here.
-- 00002 was released without dns_db_file; adopted AutoMigrate databases that
-- already applied it still lack the column, and GORM SELECT/UPDATE of
-- Settings then fails with SQLSTATE 42703.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS dns_db_file text;

-- +goose Down
-- No-op: the same column lives in 00001_baseline.sql for fresh installs.
SELECT 1;
