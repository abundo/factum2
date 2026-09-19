-- Extra VRF names are unique across all namespaces. Default VRFs keep the
-- reserved name "default" in each namespace and are excluded from this index.
--
-- +goose Up

DROP INDEX IF EXISTS public.idx_ipam_vrf_ns_name;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_vrfs_name ON public.ipam_vrfs (name) WHERE NOT is_default;

-- +goose Down

DROP INDEX IF EXISTS public.idx_ipam_vrfs_name;
CREATE UNIQUE INDEX idx_ipam_vrf_ns_name ON public.ipam_vrfs USING btree (namespace_id, name);
