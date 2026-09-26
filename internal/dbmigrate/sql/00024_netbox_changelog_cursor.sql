-- Cursor for NetBox delta sync: the newest object-change a full or delta
-- sync has applied. netbox_changelog_at is null until the first full sync.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS netbox_changelog_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS netbox_changelog_id bigint NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE public.settings DROP COLUMN IF EXISTS netbox_changelog_id;
ALTER TABLE public.settings DROP COLUMN IF EXISTS netbox_changelog_at;
