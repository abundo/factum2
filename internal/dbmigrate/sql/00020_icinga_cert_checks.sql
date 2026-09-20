-- Icinga HTTPS certificate checks: per-certificate check host, plus the
-- conf file and Jet template factum2-icinga writes on sync.
--
-- +goose Up

ALTER TABLE public.certificates
    ADD COLUMN IF NOT EXISTS host character varying(255);

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS icinga_certs_file text,
    ADD COLUMN IF NOT EXISTS icinga_cert_template text;

-- +goose Down

ALTER TABLE public.certificates
    DROP COLUMN IF EXISTS host;

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS icinga_certs_file,
    DROP COLUMN IF EXISTS icinga_cert_template;
