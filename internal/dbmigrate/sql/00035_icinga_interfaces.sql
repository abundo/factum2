-- Icinga interface monitoring: passive services written by factum2-icinga
-- sync, polled with one SNMPv2c session per device.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS icinga_monitor_interfaces boolean,
    ADD COLUMN IF NOT EXISTS icinga_interfaces_file text,
    ADD COLUMN IF NOT EXISTS icinga_snmp_communities text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS icinga_monitor_interfaces,
    DROP COLUMN IF EXISTS icinga_interfaces_file,
    DROP COLUMN IF EXISTS icinga_snmp_communities;
