-- Optional IPv4 or IPv6 address on a DNS template nameserver row.
--
-- +goose Up

ALTER TABLE public.dns_template_nameservers
    ADD COLUMN IF NOT EXISTS address character varying(64) NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE public.dns_template_nameservers
    DROP COLUMN IF EXISTS address;
