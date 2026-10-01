-- How many devices device-sync handles at once. 8 matches the previous
-- hardcoded limit, including for rows that already exist.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS device_sync_workers bigint NOT NULL DEFAULT 8;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS device_sync_workers;
