-- Allow-list of on-device services device-sync may write (eline, elan, l3vpn).
-- NULL means every implemented source, matching installs from before this column.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS device_sync_service_sources text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS device_sync_service_sources;
