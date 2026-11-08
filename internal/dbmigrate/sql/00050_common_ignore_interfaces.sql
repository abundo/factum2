-- Shared Prometheus interface ignore list. Prometheus uses its own list
-- when that list names an expression, and this list when it does not.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS common_ignore_interfaces text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS common_ignore_interfaces;
