-- Path of the dnsmgr2 zone-include YAML written by factum2-dns.
-- The rest of dnsmgr2.yaml is administrator-managed.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS dns_zones_file text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS dns_zones_file;
