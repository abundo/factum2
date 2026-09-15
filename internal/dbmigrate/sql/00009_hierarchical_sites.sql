-- Hierarchical Organization sites: one tree for NetBox regions, sites and
-- locations plus Factum-created sites. Parentage is parent_id; NetBox
-- object identity is (netbox_kind, netbox_id) because those three models
-- have independent ID sequences.
--
-- +goose Up

ALTER TABLE public.sites
    ADD COLUMN IF NOT EXISTS parent_id bigint,
    ADD COLUMN IF NOT EXISTS slug character varying(255),
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_kind character varying(32);

UPDATE public.sites
SET source = 'netbox',
    netbox_kind = 'site',
    slug = LOWER(REGEXP_REPLACE(TRIM(name), '[^a-zA-Z0-9]+', '-', 'g'))
WHERE netbox_id IS NOT NULL AND netbox_id != 0
  AND (source IS NULL OR source = '' OR netbox_kind IS NULL OR netbox_kind = '');

UPDATE public.sites
SET source = 'factum'
WHERE source IS NULL OR source = '';

DROP INDEX IF EXISTS public.idx_sites_netbox_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_netbox_kind_id
    ON public.sites (netbox_kind, netbox_id)
    WHERE netbox_id != 0;
CREATE INDEX IF NOT EXISTS idx_sites_parent_id ON public.sites (parent_id);

-- +goose Down

DROP INDEX IF EXISTS public.idx_sites_parent_id;
DROP INDEX IF EXISTS public.idx_sites_netbox_kind_id;
ALTER TABLE public.sites
    DROP COLUMN IF EXISTS parent_id,
    DROP COLUMN IF EXISTS slug,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS netbox_kind;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_netbox_id ON public.sites USING btree (netbox_id);
