-- NetBox ipam.prefix mirror. Rows are keyed by netbox_id and treated as
-- read-only in the IPAM tree. Existing allocations stay Factum-local.
--
-- +goose Up

ALTER TABLE public.ipam_prefixes
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint;

UPDATE public.ipam_prefixes SET source = 'factum' WHERE source IS NULL OR source = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_prefixes_netbox_id ON public.ipam_prefixes (netbox_id) WHERE netbox_id != 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_ipam_prefixes_netbox_id;
ALTER TABLE public.ipam_prefixes
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS netbox_id;
