-- Certificate management (lego DNS-01 / RFC2136).
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS certs_enabled boolean,
    ADD COLUMN IF NOT EXISTS certs_lego_yaml text,
    ADD COLUMN IF NOT EXISTS certs_env_file text,
    ADD COLUMN IF NOT EXISTS certs_lego_bin text,
    ADD COLUMN IF NOT EXISTS certs_lego_storage text,
    ADD COLUMN IF NOT EXISTS certs_default_key_type text,
    ADD COLUMN IF NOT EXISTS certs_default_enable_common_name boolean;

CREATE SEQUENCE IF NOT EXISTS public.cert_accounts_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.cert_accounts (
    id bigint DEFAULT nextval('public.cert_accounts_id_seq'::regclass) NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    email character varying(255),
    server character varying(512),
    key_type character varying(32),
    accepts_terms_of_service boolean NOT NULL DEFAULT false,
    eab_kid character varying(255),
    eab_hmac_key text,
    CONSTRAINT cert_accounts_pkey PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cert_accounts_name ON public.cert_accounts (name);

CREATE SEQUENCE IF NOT EXISTS public.cert_challenges_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.cert_challenges (
    id bigint DEFAULT nextval('public.cert_challenges_id_seq'::regclass) NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    kind character varying(32) NOT NULL DEFAULT 'dns-01',
    provider character varying(64) NOT NULL DEFAULT 'rfc2136',
    dns_timeout integer,
    resolvers text,
    disable_authoritative_nameservers boolean NOT NULL DEFAULT false,
    disable_recursive_nameservers boolean NOT NULL DEFAULT false,
    propagation_wait character varying(32),
    rfc2136_nameserver character varying(255),
    rfc2136_tsig_algorithm character varying(128),
    rfc2136_tsig_key character varying(255),
    rfc2136_tsig_secret text,
    rfc2136_tsig_file character varying(512),
    rfc2136_ttl integer,
    rfc2136_propagation_timeout character varying(32),
    rfc2136_polling_interval character varying(32),
    extra_env text,
    CONSTRAINT cert_challenges_pkey PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cert_challenges_name ON public.cert_challenges (name);

CREATE SEQUENCE IF NOT EXISTS public.certificates_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.certificates (
    id bigint DEFAULT nextval('public.certificates_id_seq'::regclass) NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    account_id bigint NOT NULL,
    challenge_id bigint NOT NULL,
    key_type character varying(32),
    enable_common_name boolean,
    CONSTRAINT certificates_pkey PRIMARY KEY (id),
    CONSTRAINT fk_certificates_account FOREIGN KEY (account_id) REFERENCES public.cert_accounts(id) ON UPDATE CASCADE ON DELETE RESTRICT,
    CONSTRAINT fk_certificates_challenge FOREIGN KEY (challenge_id) REFERENCES public.cert_challenges(id) ON UPDATE CASCADE ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_certificates_name ON public.certificates (name);

CREATE SEQUENCE IF NOT EXISTS public.certificate_domains_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.certificate_domains (
    id bigint DEFAULT nextval('public.certificate_domains_id_seq'::regclass) NOT NULL,
    certificate_id bigint NOT NULL,
    rank integer NOT NULL,
    name character varying(255) NOT NULL,
    CONSTRAINT certificate_domains_pkey PRIMARY KEY (id),
    CONSTRAINT fk_certificate_domains_cert FOREIGN KEY (certificate_id) REFERENCES public.certificates(id) ON UPDATE CASCADE ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cert_domain_rank ON public.certificate_domains (certificate_id, rank);

-- +goose Down

DROP TABLE IF EXISTS public.certificate_domains;
DROP TABLE IF EXISTS public.certificates;
DROP TABLE IF EXISTS public.cert_challenges;
DROP TABLE IF EXISTS public.cert_accounts;
DROP SEQUENCE IF EXISTS public.certificate_domains_id_seq;
DROP SEQUENCE IF EXISTS public.certificates_id_seq;
DROP SEQUENCE IF EXISTS public.cert_challenges_id_seq;
DROP SEQUENCE IF EXISTS public.cert_accounts_id_seq;

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS certs_enabled,
    DROP COLUMN IF EXISTS certs_lego_yaml,
    DROP COLUMN IF EXISTS certs_env_file,
    DROP COLUMN IF EXISTS certs_lego_bin,
    DROP COLUMN IF EXISTS certs_lego_storage,
    DROP COLUMN IF EXISTS certs_default_key_type,
    DROP COLUMN IF EXISTS certs_default_enable_common_name;
