-- Netbox ipam.IPAddress.dns_name, synced onto factum addresses.
--
-- +goose Up

ALTER TABLE public.addresses
    ADD COLUMN IF NOT EXISTS dns_name character varying(255);

-- +goose Down

ALTER TABLE public.addresses
    DROP COLUMN IF EXISTS dns_name;
