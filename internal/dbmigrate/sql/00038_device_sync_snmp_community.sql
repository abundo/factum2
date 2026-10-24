-- SNMPv2c community device-sync uses to read LLDP neighbors (IEEE
-- LLDP-MIB) for devices that log in with this credential row. Empty
-- means neighbor sync fails for those devices. EOS does not use it.
--
-- +goose Up

ALTER TABLE public.device_sync_auths
    ADD COLUMN IF NOT EXISTS snmp_community text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE public.device_sync_auths
    DROP COLUMN IF EXISTS snmp_community;
