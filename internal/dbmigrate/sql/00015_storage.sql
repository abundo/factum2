-- Software image repository (factum2-storage).
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS storage_enabled boolean,
    ADD COLUMN IF NOT EXISTS storage_root text,
    ADD COLUMN IF NOT EXISTS storage_http_listen text,
    ADD COLUMN IF NOT EXISTS storage_http_url text,
    ADD COLUMN IF NOT EXISTS storage_tftp_listen text,
    ADD COLUMN IF NOT EXISTS storage_tftp_host text,
    ADD COLUMN IF NOT EXISTS storage_sftp_listen text,
    ADD COLUMN IF NOT EXISTS storage_sftp_host text,
    ADD COLUMN IF NOT EXISTS storage_sftp_user text,
    ADD COLUMN IF NOT EXISTS storage_sftp_password text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS storage_enabled,
    DROP COLUMN IF EXISTS storage_root,
    DROP COLUMN IF EXISTS storage_http_listen,
    DROP COLUMN IF EXISTS storage_http_url,
    DROP COLUMN IF EXISTS storage_tftp_listen,
    DROP COLUMN IF EXISTS storage_tftp_host,
    DROP COLUMN IF EXISTS storage_sftp_listen,
    DROP COLUMN IF EXISTS storage_sftp_host,
    DROP COLUMN IF EXISTS storage_sftp_user,
    DROP COLUMN IF EXISTS storage_sftp_password;
