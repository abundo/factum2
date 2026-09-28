-- Active Directory computer account used to check MikroTik MS-CHAPv2.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS radius_machine_account text,
    ADD COLUMN IF NOT EXISTS radius_machine_password text,
    ADD COLUMN IF NOT EXISTS radius_machine_domain text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS radius_machine_account,
    DROP COLUMN IF EXISTS radius_machine_password,
    DROP COLUMN IF EXISTS radius_machine_domain;
