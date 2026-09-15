-- Link each IP address to the allocated prefix it belongs to.
-- Prefix length and VRF are read from ipam_prefixes / ipam_vrfs.
--
-- +goose Up

ALTER TABLE public.addresses
    ADD COLUMN IF NOT EXISTS prefix_id bigint;

CREATE INDEX IF NOT EXISTS idx_addresses_prefix_id ON public.addresses USING btree (prefix_id);

ALTER TABLE public.addresses
    DROP CONSTRAINT IF EXISTS fk_addresses_prefix;
ALTER TABLE public.addresses
    ADD CONSTRAINT fk_addresses_prefix FOREIGN KEY (prefix_id) REFERENCES public.ipam_prefixes(id);

-- Longest containing prefix, preferring a VRF name match when the address has one.
UPDATE public.addresses AS a
SET prefix_id = sub.prefix_id
FROM (
    SELECT DISTINCT ON (a2.id)
        a2.id AS address_id,
        p.id AS prefix_id
    FROM public.addresses a2
    JOIN public.ipam_prefixes p
        ON a2.address ~ '^[0-9a-fA-F:.]+/[0-9]+$'
        AND a2.address::inet <<= p.prefix::cidr
    LEFT JOIN public.ipam_vrfs v ON v.id = p.vrf_id
    WHERE a2.prefix_id IS NULL
      AND (
          a2.vrf IS NULL OR a2.vrf = ''
          OR LOWER(v.name) = LOWER(a2.vrf)
      )
    ORDER BY a2.id, masklen(p.prefix::cidr) DESC
) sub
WHERE a.id = sub.address_id;

-- Addresses whose VRF name did not match still get the longest covering prefix.
UPDATE public.addresses AS a
SET prefix_id = sub.prefix_id
FROM (
    SELECT DISTINCT ON (a2.id)
        a2.id AS address_id,
        p.id AS prefix_id
    FROM public.addresses a2
    JOIN public.ipam_prefixes p
        ON a2.address ~ '^[0-9a-fA-F:.]+/[0-9]+$'
        AND a2.address::inet <<= p.prefix::cidr
    WHERE a2.prefix_id IS NULL
    ORDER BY a2.id, masklen(p.prefix::cidr) DESC
) sub
WHERE a.id = sub.address_id;

-- +goose Down

ALTER TABLE public.addresses
    DROP CONSTRAINT IF EXISTS fk_addresses_prefix;
DROP INDEX IF EXISTS public.idx_addresses_prefix_id;
ALTER TABLE public.addresses
    DROP COLUMN IF EXISTS prefix_id;
