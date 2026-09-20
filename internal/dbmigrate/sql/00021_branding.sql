-- Custom header branding (logo + text next to the Factum wordmark).
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS brand_logo text,
    ADD COLUMN IF NOT EXISTS brand_text text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS brand_logo,
    DROP COLUMN IF EXISTS brand_text;
