-- Recipients for the Lime sync change report (one address per line).
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS lime_report_to text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS lime_report_to;
