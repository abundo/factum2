-- EOS ELINE checks use eAPI (show patch panel). The account is an Icinga
-- setting so the icinga worker does not read device-sync passwords.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS icinga_eapi_user text,
    ADD COLUMN IF NOT EXISTS icinga_eapi_password text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS icinga_eapi_user,
    DROP COLUMN IF EXISTS icinga_eapi_password;
