-- DNS zone editor + DHCP (v1.0.7).
--
-- 00001_baseline.sql already has this schema, so fresh installs no-op here.
-- Existing AutoMigrate databases are stamped at goose version 1 without
-- re-running the baseline; without this file, `factum2 migrate` applies
-- nothing and UPDATE settings SET dns_zones_enabled = ... fails.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS dns_zones_enabled boolean,
    ADD COLUMN IF NOT EXISTS dhcp_enabled boolean,
    ADD COLUMN IF NOT EXISTS dns_config_file text,
    ADD COLUMN IF NOT EXISTS dns_host_template text,
    ADD COLUMN IF NOT EXISTS dns_bind_type text,
    ADD COLUMN IF NOT EXISTS dns_bind_config_dir text,
    ADD COLUMN IF NOT EXISTS dns_bind_include_file text,
    ADD COLUMN IF NOT EXISTS dns_bind_zones_dir text,
    ADD COLUMN IF NOT EXISTS dns_bind_zones_file text,
    ADD COLUMN IF NOT EXISTS dns_bind_tmp_dir text,
    ADD COLUMN IF NOT EXISTS dns_bind_cmd_reload_all text,
    ADD COLUMN IF NOT EXISTS dns_bind_cmd_reload_zone text,
    ADD COLUMN IF NOT EXISTS dns_bind_cmd_restart text,
    ADD COLUMN IF NOT EXISTS dhcp_dns_servers text,
    ADD COLUMN IF NOT EXISTS dhcp_host_template text,
    ADD COLUMN IF NOT EXISTS dhcp_kea_type text,
    ADD COLUMN IF NOT EXISTS dhcp_kea4_config_dir text,
    ADD COLUMN IF NOT EXISTS dhcp_kea4_include_file text,
    ADD COLUMN IF NOT EXISTS dhcp_kea4_tmp_dir text,
    ADD COLUMN IF NOT EXISTS dhcp_kea4_cmd_restart text,
    ADD COLUMN IF NOT EXISTS dhcp_kea6_config_dir text,
    ADD COLUMN IF NOT EXISTS dhcp_kea6_include_file text,
    ADD COLUMN IF NOT EXISTS dhcp_kea6_tmp_dir text,
    ADD COLUMN IF NOT EXISTS dhcp_kea6_cmd_restart text;

ALTER TABLE IF EXISTS public.ipam_prefixes
    ADD COLUMN IF NOT EXISTS dhcp_enabled boolean,
    ADD COLUMN IF NOT EXISTS dhcp_range_start character varying(80),
    ADD COLUMN IF NOT EXISTS dhcp_range_end character varying(80),
    ADD COLUMN IF NOT EXISTS dhcp_gateway character varying(80),
    ADD COLUMN IF NOT EXISTS dhcp_dns_servers text;

CREATE SEQUENCE IF NOT EXISTS public.dns_dnssec_policies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE IF NOT EXISTS public.dns_soa_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE IF NOT EXISTS public.dns_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE IF NOT EXISTS public.dns_template_nameservers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE IF NOT EXISTS public.dns_zones_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE IF NOT EXISTS public.dns_zone_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.dns_dnssec_policies (
    id bigint NOT NULL DEFAULT nextval('public.dns_dnssec_policies_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    ksk_lifetime character varying(64),
    ksk_algorithm character varying(64),
    zsk_lifetime character varying(64),
    zsk_algorithm character varying(64),
    purge_keys character varying(64),
    signatures_validity character varying(64),
    signatures_validity_dnskey character varying(64),
    signatures_refresh character varying(64),
    CONSTRAINT dns_dnssec_policies_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.dns_soa_templates (
    id bigint NOT NULL DEFAULT nextval('public.dns_soa_templates_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    mname character varying(255),
    rname character varying(255),
    refresh bigint,
    retry bigint,
    expire bigint,
    ttl bigint,
    CONSTRAINT dns_soa_templates_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.dns_templates (
    id bigint NOT NULL DEFAULT nextval('public.dns_templates_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    soa_template_id bigint,
    default_ttl bigint,
    dns_sec_policy_id bigint,
    CONSTRAINT dns_templates_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.dns_template_nameservers (
    id bigint NOT NULL DEFAULT nextval('public.dns_template_nameservers_id_seq'::regclass),
    dns_template_id bigint NOT NULL,
    rank bigint NOT NULL,
    hostname character varying(255) NOT NULL,
    CONSTRAINT dns_template_nameservers_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.dns_zones (
    id bigint NOT NULL DEFAULT nextval('public.dns_zones_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    type character varying(16) DEFAULT 'forward'::character varying NOT NULL,
    dns_template_id bigint,
    comment text,
    CONSTRAINT dns_zones_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.dns_zone_records (
    id bigint NOT NULL DEFAULT nextval('public.dns_zone_records_id_seq'::regclass),
    dns_zone_id bigint NOT NULL,
    rank bigint NOT NULL,
    name character varying(255) NOT NULL,
    ttl bigint,
    record_type character varying(16) NOT NULL,
    value text NOT NULL,
    description text,
    mac character varying(32),
    CONSTRAINT dns_zone_records_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.dns_dnssec_policies_id_seq OWNED BY public.dns_dnssec_policies.id;
ALTER SEQUENCE public.dns_soa_templates_id_seq OWNED BY public.dns_soa_templates.id;
ALTER SEQUENCE public.dns_templates_id_seq OWNED BY public.dns_templates.id;
ALTER SEQUENCE public.dns_template_nameservers_id_seq OWNED BY public.dns_template_nameservers.id;
ALTER SEQUENCE public.dns_zones_id_seq OWNED BY public.dns_zones.id;
ALTER SEQUENCE public.dns_zone_records_id_seq OWNED BY public.dns_zone_records.id;

ALTER TABLE IF EXISTS public.dns_zone_records
    ADD COLUMN IF NOT EXISTS mac character varying(32);

CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_dnssec_policies_name ON public.dns_dnssec_policies USING btree (name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_soa_templates_name ON public.dns_soa_templates USING btree (name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_template_ns_rank ON public.dns_template_nameservers USING btree (dns_template_id, rank);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_templates_name ON public.dns_templates USING btree (name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_zone_record_rank ON public.dns_zone_records USING btree (dns_zone_id, rank);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dns_zones_name ON public.dns_zones USING btree (name);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_dns_templates_dns_sec_policy') THEN
        ALTER TABLE public.dns_templates
            ADD CONSTRAINT fk_dns_templates_dns_sec_policy
            FOREIGN KEY (dns_sec_policy_id) REFERENCES public.dns_dnssec_policies(id)
            ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_dns_templates_soa_template') THEN
        ALTER TABLE public.dns_templates
            ADD CONSTRAINT fk_dns_templates_soa_template
            FOREIGN KEY (soa_template_id) REFERENCES public.dns_soa_templates(id)
            ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_dns_templates_nameservers') THEN
        ALTER TABLE public.dns_template_nameservers
            ADD CONSTRAINT fk_dns_templates_nameservers
            FOREIGN KEY (dns_template_id) REFERENCES public.dns_templates(id)
            ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_dns_zones_dns_template') THEN
        ALTER TABLE public.dns_zones
            ADD CONSTRAINT fk_dns_zones_dns_template
            FOREIGN KEY (dns_template_id) REFERENCES public.dns_templates(id)
            ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_dns_zones_records') THEN
        ALTER TABLE public.dns_zone_records
            ADD CONSTRAINT fk_dns_zones_records
            FOREIGN KEY (dns_zone_id) REFERENCES public.dns_zones(id)
            ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- No-op: the same objects live in 00001_baseline.sql for fresh installs.
SELECT 1;
