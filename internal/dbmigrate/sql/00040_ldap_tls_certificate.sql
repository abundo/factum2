-- LDAPS can present a certificate from the certificate store instead of
-- the worker hub certificate.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS ldap_internal_tls_certificate_id bigint;

ALTER TABLE public.settings
    DROP CONSTRAINT IF EXISTS fk_settings_ldap_internal_tls_certificate;
ALTER TABLE public.settings
    ADD CONSTRAINT fk_settings_ldap_internal_tls_certificate
    FOREIGN KEY (ldap_internal_tls_certificate_id) REFERENCES public.certificates(id)
    ON UPDATE CASCADE ON DELETE SET NULL;

-- +goose Down

ALTER TABLE public.settings
    DROP CONSTRAINT IF EXISTS fk_settings_ldap_internal_tls_certificate;
ALTER TABLE public.settings
    DROP COLUMN IF EXISTS ldap_internal_tls_certificate_id;
