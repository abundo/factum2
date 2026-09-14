-- Shared DCIM catalog: source + netbox_id so NetBox sync and Factum-local
-- rows live in the same manufacturer / device_type / platform tables.
--
-- +goose Up

ALTER TABLE public.manufacturers
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint;
UPDATE public.manufacturers SET source = 'factum' WHERE source IS NULL OR source = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_manufacturers_netbox_id ON public.manufacturers (netbox_id) WHERE netbox_id != 0;

ALTER TABLE public.device_types
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint;
UPDATE public.device_types SET source = 'factum' WHERE source IS NULL OR source = '';
DROP INDEX IF EXISTS public.idx_device_types_slug;
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_netbox_id ON public.device_types (netbox_id) WHERE netbox_id != 0;

ALTER TABLE public.platforms
    ADD COLUMN IF NOT EXISTS source character varying(32),
    ADD COLUMN IF NOT EXISTS netbox_id bigint;
UPDATE public.platforms SET source = 'factum' WHERE source IS NULL OR source = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_platforms_netbox_id ON public.platforms (netbox_id) WHERE netbox_id != 0;

-- Backfill from already-synced devices so the DCIM lists are populated
-- before the next NetBox job runs. GROUP BY slug/model so mixed
-- factum+netbox devices with the same name do not hit ON CONFLICT twice.
INSERT INTO public.manufacturers (created_at, updated_at, name, slug, source, netbox_id)
SELECT NOW(), NOW(),
       MIN(d.manufacturer),
       LOWER(REGEXP_REPLACE(TRIM(MIN(d.manufacturer)), '[^a-zA-Z0-9]+', '-', 'g')),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 'factum' ELSE 'netbox' END),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 0 ELSE COALESCE(d.manufacturer_id, 0) END)
FROM public.devices d
WHERE d.manufacturer IS NOT NULL AND d.manufacturer <> ''
GROUP BY LOWER(REGEXP_REPLACE(TRIM(d.manufacturer), '[^a-zA-Z0-9]+', '-', 'g'))
ON CONFLICT (slug) DO UPDATE
SET netbox_id = CASE WHEN public.manufacturers.netbox_id != 0 THEN public.manufacturers.netbox_id ELSE EXCLUDED.netbox_id END,
    source = CASE WHEN public.manufacturers.source = 'netbox' THEN public.manufacturers.source ELSE EXCLUDED.source END;

INSERT INTO public.device_types (created_at, updated_at, manufacturer_id, model, slug, source, netbox_id)
SELECT NOW(), NOW(), m.id, MIN(d.model_name),
       LOWER(REGEXP_REPLACE(TRIM(MIN(d.model_name)), '[^a-zA-Z0-9]+', '-', 'g')),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 'factum' ELSE 'netbox' END),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 0 ELSE COALESCE(d.model_id, 0) END)
FROM public.devices d
JOIN public.manufacturers m ON m.name = d.manufacturer
WHERE d.model_name IS NOT NULL AND d.model_name <> ''
GROUP BY m.id, d.model_name
ON CONFLICT (manufacturer_id, model) DO UPDATE
SET netbox_id = CASE WHEN public.device_types.netbox_id != 0 THEN public.device_types.netbox_id ELSE EXCLUDED.netbox_id END,
    source = CASE WHEN public.device_types.source = 'netbox' THEN public.device_types.source ELSE EXCLUDED.source END;

INSERT INTO public.platforms (created_at, updated_at, name, slug, source, netbox_id)
SELECT NOW(), NOW(),
       MIN(d.platform),
       LOWER(REGEXP_REPLACE(TRIM(MIN(d.platform)), '[^a-zA-Z0-9]+', '-', 'g')),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 'factum' ELSE 'netbox' END),
       MAX(CASE WHEN d.cf_source = 'factum' THEN 0 ELSE COALESCE(d.platform_id, 0) END)
FROM public.devices d
WHERE d.platform IS NOT NULL AND d.platform <> ''
GROUP BY LOWER(REGEXP_REPLACE(TRIM(d.platform), '[^a-zA-Z0-9]+', '-', 'g'))
ON CONFLICT (slug) DO UPDATE
SET netbox_id = CASE WHEN public.platforms.netbox_id != 0 THEN public.platforms.netbox_id ELSE EXCLUDED.netbox_id END,
    source = CASE WHEN public.platforms.source = 'netbox' THEN public.platforms.source ELSE EXCLUDED.source END;

-- +goose Down

DROP INDEX IF EXISTS public.idx_platforms_netbox_id;
ALTER TABLE public.platforms DROP COLUMN IF EXISTS source, DROP COLUMN IF EXISTS netbox_id;

DROP INDEX IF EXISTS public.idx_device_types_netbox_id;
ALTER TABLE public.device_types DROP COLUMN IF EXISTS source, DROP COLUMN IF EXISTS netbox_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_types_slug ON public.device_types (slug);

DROP INDEX IF EXISTS public.idx_manufacturers_netbox_id;
ALTER TABLE public.manufacturers DROP COLUMN IF EXISTS source, DROP COLUMN IF EXISTS netbox_id;
