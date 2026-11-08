-- NetBox ASN ranges, ASNs, and route targets. Rows are keyed by netbox_id.
-- An empty fetch does not delete. Factum-local rows (netbox_id 0) stay.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS public.ipam_asn_ranges (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name character varying(255) NOT NULL,
    slug character varying(255),
    rir character varying(255),
    start bigint,
    "end" bigint,
    tenant character varying(255),
    description character varying(255),
    source character varying(32),
    netbox_id bigint
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_asn_ranges_netbox_id
    ON public.ipam_asn_ranges (netbox_id) WHERE netbox_id != 0;

CREATE TABLE IF NOT EXISTS public.ipam_asns (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    asn bigint,
    rir character varying(255),
    role character varying(255),
    tenant character varying(255),
    description character varying(255),
    sites text,
    source character varying(32),
    netbox_id bigint
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_asns_netbox_id
    ON public.ipam_asns (netbox_id) WHERE netbox_id != 0;

CREATE TABLE IF NOT EXISTS public.ipam_route_targets (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name character varying(255) NOT NULL,
    tenant character varying(255),
    description character varying(255),
    source character varying(32),
    netbox_id bigint
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ipam_route_targets_netbox_id
    ON public.ipam_route_targets (netbox_id) WHERE netbox_id != 0;

-- +goose Down

DROP TABLE IF EXISTS public.ipam_route_targets;
DROP TABLE IF EXISTS public.ipam_asns;
DROP TABLE IF EXISTS public.ipam_asn_ranges;
