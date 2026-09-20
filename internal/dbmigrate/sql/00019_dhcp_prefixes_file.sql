-- Path of the dnsmgr2 prefix-include YAML written by factum2-dns.
-- Kea host templates stay in the administrator-managed dnsmgr2.yaml.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS dhcp_prefixes_file text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS dhcp_prefixes_file;
