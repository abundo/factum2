-- Local DCIM catalog (manufacturer, device type, platform) and allow many
-- Factum-created devices with netbox_id = 0.
--
-- +goose Up

CREATE SEQUENCE IF NOT EXISTS public.manufacturers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.manufacturers (
    id bigint NOT NULL DEFAULT nextval('public.manufacturers_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    slug character varying(255) NOT NULL,
    CONSTRAINT manufacturers_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.manufacturers_id_seq OWNED BY public.manufacturers.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_manufacturers_name ON public.manufacturers (name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_manufacturers_slug ON public.manufacturers (slug);

CREATE SEQUENCE IF NOT EXISTS public.device_types_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.device_types (
    id bigint NOT NULL DEFAULT nextval('public.device_types_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    manufacturer_id bigint NOT NULL,
    model character varying(255) NOT NULL,
    slug character varying(255) NOT NULL,
    CONSTRAINT device_types_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.device_types_id_seq OWNED BY public.device_types.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_slug ON public.device_types (slug);
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_manufacturer_model ON public.device_types (manufacturer_id, model);
CREATE INDEX IF NOT EXISTS idx_device_types_manufacturer_id ON public.device_types (manufacturer_id);

CREATE SEQUENCE IF NOT EXISTS public.platforms_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.platforms (
    id bigint NOT NULL DEFAULT nextval('public.platforms_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    slug character varying(255) NOT NULL,
    manufacturer_id bigint,
    CONSTRAINT platforms_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.platforms_id_seq OWNED BY public.platforms.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_platforms_name ON public.platforms (name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_platforms_slug ON public.platforms (slug);

DROP INDEX IF EXISTS public.idx_devices_netbox_id_vm;
CREATE UNIQUE INDEX idx_devices_netbox_id_vm ON public.devices USING btree (vm, netbox_id) WHERE netbox_id != 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_devices_netbox_id_vm;
CREATE UNIQUE INDEX idx_devices_netbox_id_vm ON public.devices USING btree (vm, netbox_id);

DROP TABLE IF EXISTS public.platforms;
DROP SEQUENCE IF EXISTS public.platforms_id_seq;
DROP TABLE IF EXISTS public.device_types;
DROP SEQUENCE IF EXISTS public.device_types_id_seq;
DROP TABLE IF EXISTS public.manufacturers;
DROP SEQUENCE IF EXISTS public.manufacturers_id_seq;
