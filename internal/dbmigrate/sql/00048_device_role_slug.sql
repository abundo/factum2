-- A Factum-only device role and a NetBox role may share a slug.
-- Each source still has unique slugs.
--
-- +goose Up

DROP INDEX IF EXISTS public.idx_device_roles_slug;
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_roles_slug_factum
    ON public.device_roles (slug) WHERE source IS DISTINCT FROM 'netbox';
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_roles_slug_netbox
    ON public.device_roles (slug) WHERE source = 'netbox';

-- +goose Down

DROP INDEX IF EXISTS public.idx_device_roles_slug_factum;
DROP INDEX IF EXISTS public.idx_device_roles_slug_netbox;
CREATE UNIQUE INDEX IF NOT EXISTS idx_device_roles_slug ON public.device_roles (slug);
