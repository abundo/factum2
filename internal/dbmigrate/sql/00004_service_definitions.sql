-- Service definitions: homogeneous interfaces spec, connection types,
-- applied snapshot on endpoints. Breaking one-shot wipe of catalog types,
-- translation CLI, and all services (lab/dev; next Lime sync recreates
-- commercial rows). Seed does not repeat this wipe.
--
-- +goose Up

-- Additive schema first so later DELETEs can mention new tables.

CREATE SEQUENCE IF NOT EXISTS public.service_connection_types_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.service_connection_types (
    id bigint NOT NULL DEFAULT nextval('public.service_connection_types_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    service_type_id bigint NOT NULL,
    name character varying(64) NOT NULL,
    sort_order bigint,
    image bytea,
    content_type character varying(64),
    CONSTRAINT service_connection_types_pkey PRIMARY KEY (id),
    CONSTRAINT idx_svc_ct_type_name UNIQUE (service_type_id, name) DEFERRABLE INITIALLY DEFERRED
);

ALTER SEQUENCE public.service_connection_types_id_seq OWNED BY public.service_connection_types.id;

ALTER TABLE public.service_types
    ADD COLUMN IF NOT EXISTS interfaces text;

ALTER TABLE public.service_endpoints
    ADD COLUMN IF NOT EXISTS applied_device_id bigint,
    ADD COLUMN IF NOT EXISTS applied_iface text,
    ADD COLUMN IF NOT EXISTS applied_platform text,
    ADD COLUMN IF NOT EXISTS applied_fields text;

ALTER TABLE public.services
    ADD COLUMN IF NOT EXISTS connection_type_id bigint;

-- +goose StatementBegin
DO $$
DECLARE
    deleted_services bigint;
    deleted_types bigint;
    deleted_translation_cli bigint;
BEGIN
    UPDATE public.maintenance_notifications SET service_ids = '[]';

    DELETE FROM public.service_hops WHERE service_id IN (SELECT id FROM public.services);
    DELETE FROM public.service_paths WHERE service_id IN (SELECT id FROM public.services);

    DELETE FROM public.service_endpoints;

    DELETE FROM public.config_cli_features WHERE scope_id IN (
        SELECT id FROM public.config_scopes WHERE kind = 'cli' AND service_type_id IS NOT NULL
    );
    DELETE FROM public.config_assignments WHERE scope_id IN (
        SELECT id FROM public.config_scopes WHERE kind = 'cli' AND service_type_id IS NOT NULL
    );

    WITH RECURSIVE svc_tree AS (
        SELECT id FROM public.config_scopes WHERE kind IN ('service', 'service_endpoint')
        UNION ALL
        SELECT s.id FROM public.config_scopes s JOIN svc_tree t ON s.parent_id = t.id
    )
    , del_feat AS (
        DELETE FROM public.config_cli_features WHERE scope_id IN (SELECT id FROM svc_tree)
    )
    , del_asg AS (
        DELETE FROM public.config_assignments WHERE scope_id IN (SELECT id FROM svc_tree)
    )
    DELETE FROM public.config_scopes WHERE id IN (SELECT id FROM svc_tree);

    SELECT COUNT(*) INTO deleted_translation_cli
    FROM public.config_scopes
    WHERE kind = 'cli' AND service_type_id IS NOT NULL;

    DELETE FROM public.config_scopes
    WHERE kind = 'cli' AND service_type_id IS NOT NULL;

    -- Empty _catalog/cli/<Name> folders left after translation CLI wipe.
    DELETE FROM public.config_scopes child
    USING public.config_scopes cli, public.config_scopes catalog, public.config_scopes root
    WHERE child.parent_id = cli.id
      AND cli.parent_id = catalog.id
      AND catalog.parent_id = root.id
      AND root.parent_id IS NULL AND root.name = 'global'
      AND catalog.name = '_catalog'
      AND cli.name = 'cli'
      AND child.kind = 'folder'
      AND NOT EXISTS (SELECT 1 FROM public.config_scopes x WHERE x.parent_id = child.id);

    SELECT COUNT(*) INTO deleted_services FROM public.services;
    DELETE FROM public.services;

    DELETE FROM public.service_connection_types;

    SELECT COUNT(*) INTO deleted_types FROM public.service_types;
    DELETE FROM public.service_types;

    RAISE NOTICE 'deleted_services=% deleted_types=% deleted_translation_cli=%',
        deleted_services, deleted_types, deleted_translation_cli;
END $$;
-- +goose StatementEnd

ALTER TABLE public.service_types
    DROP COLUMN IF EXISTS endpoint_roles;

ALTER TABLE public.services
    DROP COLUMN IF EXISTS endpoint_a_device_id,
    DROP COLUMN IF EXISTS endpoint_a_interface_id,
    DROP COLUMN IF EXISTS endpoint_a_vlan,
    DROP COLUMN IF EXISTS endpoint_a_subinterface_netbox_id,
    DROP COLUMN IF EXISTS endpoint_a_termination_netbox_id,
    DROP COLUMN IF EXISTS endpoint_b_device_id,
    DROP COLUMN IF EXISTS endpoint_b_interface_id,
    DROP COLUMN IF EXISTS endpoint_b_vlan,
    DROP COLUMN IF EXISTS endpoint_b_subinterface_netbox_id,
    DROP COLUMN IF EXISTS endpoint_b_termination_netbox_id,
    DROP COLUMN IF EXISTS applied_endpoint_a_device_id,
    DROP COLUMN IF EXISTS applied_endpoint_a_iface,
    DROP COLUMN IF EXISTS applied_endpoint_a_vlan,
    DROP COLUMN IF EXISTS applied_endpoint_b_device_id,
    DROP COLUMN IF EXISTS applied_endpoint_b_iface,
    DROP COLUMN IF EXISTS applied_endpoint_b_vlan;

-- +goose Down
-- Breaking wipe; rollback is a Postgres restore, not this Down.
SELECT 1;
