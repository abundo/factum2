-- Shared destination ignore lists. A destination uses its own list when
-- that list names an entry, and the matching common list when it does not.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS common_ignore_devices text,
    ADD COLUMN IF NOT EXISTS common_ignore_models text,
    ADD COLUMN IF NOT EXISTS common_ignore_platforms text,
    ADD COLUMN IF NOT EXISTS common_ignore_manufacturers text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS common_ignore_manufacturers,
    DROP COLUMN IF EXISTS common_ignore_platforms,
    DROP COLUMN IF EXISTS common_ignore_models,
    DROP COLUMN IF EXISTS common_ignore_devices;
