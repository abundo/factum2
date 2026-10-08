-- NetBox region/site/location rows mirrored into the config tree.
-- Identity is (netbox_kind, netbox_id); those three NetBox models do not
-- share an ID sequence.
--
-- +goose Up

ALTER TABLE public.config_scopes
    ADD COLUMN IF NOT EXISTS netbox_kind character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_config_scopes_netbox_kind_id
    ON public.config_scopes (netbox_kind, netbox_id)
    WHERE netbox_id != 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_config_scopes_netbox_kind_id;
ALTER TABLE public.config_scopes
    DROP COLUMN IF EXISTS netbox_id,
    DROP COLUMN IF EXISTS netbox_kind;
