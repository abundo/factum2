-- Device interfaces as a DCIM resource, interface templates on device
-- types, and allow many Factum-local interfaces (netbox_id = 0) per device.
--
-- +goose Up

ALTER TABLE public.devices
    ADD COLUMN IF NOT EXISTS device_type_id bigint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_devices_device_type_id ON public.devices (device_type_id);

UPDATE public.devices d
SET device_type_id = dt.id
FROM public.device_types dt
JOIN public.manufacturers m ON m.id = dt.manufacturer_id
WHERE d.device_type_id = 0
  AND d.manufacturer IS NOT NULL AND d.manufacturer <> ''
  AND d.model_name IS NOT NULL AND d.model_name <> ''
  AND d.manufacturer = m.name
  AND d.model_name = dt.model;

DROP INDEX IF EXISTS public.idx_interfaces_device_id_netbox_id;
CREATE UNIQUE INDEX idx_interfaces_device_id_netbox_id ON public.interfaces (device_id, netbox_id) WHERE netbox_id != 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_interfaces_device_id_name ON public.interfaces (device_id, name);

CREATE SEQUENCE IF NOT EXISTS public.interface_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.interface_templates (
    id bigint NOT NULL DEFAULT nextval('public.interface_templates_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_type_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    type character varying(255),
    label character varying(255),
    description character varying(255),
    source character varying(32),
    netbox_id bigint,
    CONSTRAINT interface_templates_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.interface_templates_id_seq OWNED BY public.interface_templates.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_interface_templates_type_name ON public.interface_templates (device_type_id, name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_interface_templates_netbox_id ON public.interface_templates (netbox_id) WHERE netbox_id != 0;
CREATE INDEX IF NOT EXISTS idx_interface_templates_device_type_id ON public.interface_templates (device_type_id);

-- +goose Down

DROP TABLE IF EXISTS public.interface_templates;
DROP SEQUENCE IF EXISTS public.interface_templates_id_seq;

DROP INDEX IF EXISTS public.idx_interfaces_device_id_name;
DROP INDEX IF EXISTS public.idx_interfaces_device_id_netbox_id;
CREATE UNIQUE INDEX idx_interfaces_device_id_netbox_id ON public.interfaces (device_id, netbox_id);

DROP INDEX IF EXISTS public.idx_devices_device_type_id;
ALTER TABLE public.devices DROP COLUMN IF EXISTS device_type_id;
