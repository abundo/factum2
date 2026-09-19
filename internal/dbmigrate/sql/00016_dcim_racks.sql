-- Datacenter racks, placements, floor plans and per-user connection layouts.
--
-- +goose Up

ALTER TABLE public.device_types
    ADD COLUMN IF NOT EXISTS height_ticks integer,
    ADD COLUMN IF NOT EXISTS full_depth boolean,
    ADD COLUMN IF NOT EXISTS front_image character varying(512),
    ADD COLUMN IF NOT EXISTS rear_image character varying(512);

CREATE SEQUENCE IF NOT EXISTS public.racks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.racks (
    id bigint NOT NULL DEFAULT nextval('public.racks_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    site_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    source character varying(32),
    netbox_id bigint,
    height_u integer NOT NULL DEFAULT 42,
    width_mm integer,
    depth_mm integer,
    start_unit integer NOT NULL DEFAULT 1,
    numbering character varying(32) NOT NULL DEFAULT 'ascending',
    version integer NOT NULL DEFAULT 1,
    CONSTRAINT racks_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.racks_id_seq OWNED BY public.racks.id;

CREATE INDEX IF NOT EXISTS idx_racks_site_id ON public.racks (site_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_racks_netbox_id ON public.racks (netbox_id) WHERE netbox_id != 0;

CREATE SEQUENCE IF NOT EXISTS public.device_placements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.device_placements (
    id bigint NOT NULL DEFAULT nextval('public.device_placements_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_id bigint NOT NULL,
    rack_id bigint NOT NULL,
    offset_ticks integer NOT NULL DEFAULT 0,
    face character varying(16) NOT NULL DEFAULT 'front',
    source character varying(32),
    version integer NOT NULL DEFAULT 1,
    conflict character varying(64),
    CONSTRAINT device_placements_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.device_placements_id_seq OWNED BY public.device_placements.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_device_placements_device_id ON public.device_placements (device_id);
CREATE INDEX IF NOT EXISTS idx_device_placements_rack_id ON public.device_placements (rack_id);

CREATE SEQUENCE IF NOT EXISTS public.floor_plans_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.floor_plans (
    id bigint NOT NULL DEFAULT nextval('public.floor_plans_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    site_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    width_mm integer NOT NULL DEFAULT 20000,
    height_mm integer NOT NULL DEFAULT 15000,
    grid_mm integer NOT NULL DEFAULT 600,
    revision integer NOT NULL DEFAULT 1,
    CONSTRAINT floor_plans_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.floor_plans_id_seq OWNED BY public.floor_plans.id;

CREATE INDEX IF NOT EXISTS idx_floor_plans_site_id ON public.floor_plans (site_id);

CREATE SEQUENCE IF NOT EXISTS public.floor_plan_racks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.floor_plan_racks (
    id bigint NOT NULL DEFAULT nextval('public.floor_plan_racks_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    floor_plan_id bigint NOT NULL,
    rack_id bigint NOT NULL,
    x_mm integer NOT NULL DEFAULT 0,
    y_mm integer NOT NULL DEFAULT 0,
    rotation integer NOT NULL DEFAULT 0,
    CONSTRAINT floor_plan_racks_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.floor_plan_racks_id_seq OWNED BY public.floor_plan_racks.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_floor_plan_racks_plan_rack ON public.floor_plan_racks (floor_plan_id, rack_id);
CREATE INDEX IF NOT EXISTS idx_floor_plan_racks_rack_id ON public.floor_plan_racks (rack_id);

CREATE SEQUENCE IF NOT EXISTS public.floor_plan_annotations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.floor_plan_annotations (
    id bigint NOT NULL DEFAULT nextval('public.floor_plan_annotations_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    floor_plan_id bigint NOT NULL,
    kind character varying(32) NOT NULL,
    text character varying(255),
    x_mm integer NOT NULL DEFAULT 0,
    y_mm integer NOT NULL DEFAULT 0,
    width_mm integer NOT NULL DEFAULT 0,
    height_mm integer NOT NULL DEFAULT 0,
    rotation integer NOT NULL DEFAULT 0,
    CONSTRAINT floor_plan_annotations_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.floor_plan_annotations_id_seq OWNED BY public.floor_plan_annotations.id;

CREATE INDEX IF NOT EXISTS idx_floor_plan_annotations_floor_plan_id ON public.floor_plan_annotations (floor_plan_id);

CREATE SEQUENCE IF NOT EXISTS public.connection_view_layouts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.connection_view_layouts (
    id bigint NOT NULL DEFAULT nextval('public.connection_view_layouts_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint NOT NULL,
    scope character varying(64) NOT NULL,
    revision integer NOT NULL DEFAULT 1,
    nodes text,
    CONSTRAINT connection_view_layouts_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.connection_view_layouts_id_seq OWNED BY public.connection_view_layouts.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_connection_view_layouts_user_scope
    ON public.connection_view_layouts (user_id, scope);

-- +goose Down

DROP TABLE IF EXISTS public.connection_view_layouts;
DROP TABLE IF EXISTS public.floor_plan_annotations;
DROP TABLE IF EXISTS public.floor_plan_racks;
DROP TABLE IF EXISTS public.floor_plans;
DROP TABLE IF EXISTS public.device_placements;
DROP TABLE IF EXISTS public.racks;

ALTER TABLE public.device_types
    DROP COLUMN IF EXISTS height_ticks,
    DROP COLUMN IF EXISTS full_depth,
    DROP COLUMN IF EXISTS front_image,
    DROP COLUMN IF EXISTS rear_image;
