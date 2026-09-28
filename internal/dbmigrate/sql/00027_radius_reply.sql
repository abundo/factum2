-- Access-Accept attributes (vendor AV pairs) for RADIUS network login.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS radius_reply text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS radius_reply;
