-- Interface type catalog (NetBox dcim.Interface.type choices, plus Factum-local).
--
-- +goose Up

CREATE SEQUENCE IF NOT EXISTS public.interface_types_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE IF NOT EXISTS public.interface_types (
    id bigint NOT NULL DEFAULT nextval('public.interface_types_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    value character varying(255) NOT NULL,
    label character varying(255) NOT NULL,
    source character varying(32),
    sort_order integer NOT NULL DEFAULT 0,
    CONSTRAINT interface_types_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.interface_types_id_seq OWNED BY public.interface_types.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_interface_types_value ON public.interface_types (value);

INSERT INTO public.interface_types (created_at, updated_at, value, label, source, sort_order)
VALUES
    (NOW(), NOW(), '1000base-t', '1000BASE-T (1GE)', 'factum', 0),
    (NOW(), NOW(), '1000base-x-sfp', 'SFP (1GE)', 'factum', 1),
    (NOW(), NOW(), '10gbase-t', '10GBASE-T (10GE)', 'factum', 2),
    (NOW(), NOW(), '10gbase-x-sfpp', 'SFP+ (10GE)', 'factum', 3),
    (NOW(), NOW(), '25gbase-x-sfp28', 'SFP28 (25GE)', 'factum', 4),
    (NOW(), NOW(), '40gbase-x-qsfpp', 'QSFP+ (40GE)', 'factum', 5),
    (NOW(), NOW(), '100gbase-x-qsfp28', 'QSFP28 (100GE)', 'factum', 6),
    (NOW(), NOW(), 'lag', 'Link Aggregation Group (LAG)', 'factum', 7),
    (NOW(), NOW(), 'virtual', 'Virtual', 'factum', 8),
    (NOW(), NOW(), 'other', 'Other', 'factum', 9)
ON CONFLICT (value) DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS public.interface_types;
