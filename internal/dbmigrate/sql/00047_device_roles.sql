-- NetBox device roles (dcim.DeviceRole). Device.role_id stays the NetBox
-- id. The next full sync replaces the backfilled slug with NetBox's slug.
--
-- +goose Up

CREATE SEQUENCE IF NOT EXISTS public.device_roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.device_roles (
    id bigint NOT NULL DEFAULT nextval('public.device_roles_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    slug character varying(255) NOT NULL,
    color character varying(6) NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    vm_role boolean NOT NULL DEFAULT false,
    source character varying(32),
    netbox_id bigint NOT NULL DEFAULT 0,
    CONSTRAINT device_roles_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.device_roles_id_seq OWNED BY public.device_roles.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_device_roles_slug ON public.device_roles (slug);
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_roles_netbox_id
    ON public.device_roles (netbox_id) WHERE netbox_id != 0;

INSERT INTO public.device_roles (created_at, updated_at, name, slug, source, netbox_id)
SELECT NOW(), NOW(),
       MIN(d.role),
       LEFT(
           trim(both '-' from lower(regexp_replace(MIN(d.role), '[^a-zA-Z0-9]+', '-', 'g')))
               || '-' || d.role_id::text,
           255
       ),
       'netbox',
       d.role_id
FROM public.devices d
WHERE d.role <> '' AND d.role_id <> 0
GROUP BY d.role_id
ON CONFLICT DO NOTHING;

-- +goose Down

DROP INDEX IF EXISTS public.idx_device_roles_netbox_id;
DROP INDEX IF EXISTS public.idx_device_roles_slug;
DROP TABLE IF EXISTS public.device_roles;
DROP SEQUENCE IF EXISTS public.device_roles_id_seq;
