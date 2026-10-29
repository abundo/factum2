-- NetBox prefixes share one namespace and may repeat a CIDR across VRFs.
-- Factum-created prefixes stay unique per namespace. Unique IP Space is
-- stored on the VRF.
--
-- +goose Up

ALTER TABLE public.ipam_vrfs
    ADD COLUMN IF NOT EXISTS enforce_unique boolean NOT NULL DEFAULT true;

DROP INDEX IF EXISTS public.idx_ipam_alloc_ns_pfx;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_alloc_ns_pfx
    ON public.ipam_prefixes (namespace_id, prefix)
    WHERE COALESCE(netbox_id, 0) = 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_ipam_alloc_ns_pfx;
CREATE UNIQUE INDEX idx_ipam_alloc_ns_pfx ON public.ipam_prefixes USING btree (namespace_id, prefix);

ALTER TABLE public.ipam_vrfs DROP COLUMN IF EXISTS enforce_unique;
