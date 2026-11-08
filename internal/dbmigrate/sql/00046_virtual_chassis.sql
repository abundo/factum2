-- NetBox virtual chassis. A ROADM is one chassis: the master device is
-- the shelf controller and each other member is one degree.
--
-- +goose Up

CREATE SEQUENCE IF NOT EXISTS public.virtual_chassis_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.virtual_chassis (
    id bigint NOT NULL DEFAULT nextval('public.virtual_chassis_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    netbox_id bigint NOT NULL DEFAULT 0,
    name character varying(255) NOT NULL DEFAULT '',
    domain character varying(255) NOT NULL DEFAULT '',
    master_netbox_id bigint NOT NULL DEFAULT 0,
    master_device_id bigint NOT NULL DEFAULT 0,
    CONSTRAINT virtual_chassis_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.virtual_chassis_id_seq OWNED BY public.virtual_chassis.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_chassis_netbox_id
    ON public.virtual_chassis (netbox_id);
CREATE INDEX IF NOT EXISTS idx_virtual_chassis_master_device_id
    ON public.virtual_chassis (master_device_id);

ALTER TABLE public.devices
    ADD COLUMN IF NOT EXISTS virtual_chassis_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS vc_position integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS vc_priority integer NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_devices_virtual_chassis_id
    ON public.devices (virtual_chassis_id);

-- +goose Down

DROP INDEX IF EXISTS public.idx_devices_virtual_chassis_id;
ALTER TABLE public.devices
    DROP COLUMN IF EXISTS vc_priority,
    DROP COLUMN IF EXISTS vc_position,
    DROP COLUMN IF EXISTS virtual_chassis_id;

DROP INDEX IF EXISTS public.idx_virtual_chassis_master_device_id;
DROP INDEX IF EXISTS public.idx_virtual_chassis_netbox_id;
DROP TABLE IF EXISTS public.virtual_chassis;
DROP SEQUENCE IF EXISTS public.virtual_chassis_id_seq;
