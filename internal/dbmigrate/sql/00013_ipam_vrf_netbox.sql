-- VRF route distinguisher / route-targets, plus NetBox sync origin.
-- NetBox-imported VRFs are keyed by netbox_id and treated as read-only.
--
-- +goose Up

ALTER TABLE public.ipam_vrfs
    ADD COLUMN IF NOT EXISTS rd character varying(255),
    ADD COLUMN IF NOT EXISTS import_rt text,
    ADD COLUMN IF NOT EXISTS export_rt text,
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint;

UPDATE public.ipam_vrfs SET source = 'factum' WHERE source IS NULL OR source = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_vrfs_netbox_id ON public.ipam_vrfs (netbox_id) WHERE netbox_id != 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_ipam_vrfs_netbox_id;
ALTER TABLE public.ipam_vrfs
    DROP COLUMN IF EXISTS rd,
    DROP COLUMN IF EXISTS import_rt,
    DROP COLUMN IF EXISTS export_rt,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS netbox_id;
