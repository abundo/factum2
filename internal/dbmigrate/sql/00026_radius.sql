-- RADIUS network login: NAS clients, LDAP group to device-role policy,
-- and the accept/reject log shipped by radius workers.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS radius_enabled boolean,
    ADD COLUMN IF NOT EXISTS radius_listen text;

CREATE SEQUENCE IF NOT EXISTS public.radius_clients_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.radius_clients (
    id bigint NOT NULL DEFAULT nextval('public.radius_clients_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    address character varying(64) NOT NULL,
    secret text NOT NULL,
    enabled boolean,
    CONSTRAINT radius_clients_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.radius_clients_id_seq OWNED BY public.radius_clients.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_radius_clients_address ON public.radius_clients (address);

CREATE SEQUENCE IF NOT EXISTS public.radius_policies_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.radius_policies (
    id bigint NOT NULL DEFAULT nextval('public.radius_policies_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    group_dn character varying(512) NOT NULL,
    all_devices boolean,
    roles text,
    CONSTRAINT radius_policies_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.radius_policies_id_seq OWNED BY public.radius_policies.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_radius_policies_group_dn ON public.radius_policies (group_dn);

CREATE SEQUENCE IF NOT EXISTS public.radius_events_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.radius_events (
    id bigint NOT NULL DEFAULT nextval('public.radius_events_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    reported_at timestamp with time zone,
    username character varying(255),
    nas_ip character varying(64),
    device_name character varying(255),
    device_role character varying(255),
    result character varying(16),
    reason character varying(255),
    worker character varying(255),
    CONSTRAINT radius_events_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.radius_events_id_seq OWNED BY public.radius_events.id;

CREATE INDEX IF NOT EXISTS idx_radius_events_username ON public.radius_events (username);
CREATE INDEX IF NOT EXISTS idx_radius_events_result ON public.radius_events (result);

-- +goose Down

DROP TABLE IF EXISTS public.radius_events;
DROP TABLE IF EXISTS public.radius_policies;
DROP TABLE IF EXISTS public.radius_clients;

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS radius_enabled,
    DROP COLUMN IF EXISTS radius_listen;
