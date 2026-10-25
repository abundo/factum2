-- Internal LDAP directory: organizational units for people and roles,
-- and nested role membership (a role can contain other roles).
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS ldap_internal_enabled boolean,
    ADD COLUMN IF NOT EXISTS ldap_internal_listen text,
    ADD COLUMN IF NOT EXISTS ldap_internal_tls_listen text,
    ADD COLUMN IF NOT EXISTS ldap_internal_base_dn text,
    ADD COLUMN IF NOT EXISTS ldap_internal_bind_password text,
    ADD COLUMN IF NOT EXISTS radius_use_internal_ldap boolean;

ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS ldap_ou_id bigint;

ALTER TABLE public.roles
    ADD COLUMN IF NOT EXISTS ldap_ou_id bigint;

CREATE INDEX IF NOT EXISTS idx_users_ldap_ou_id ON public.users (ldap_ou_id);
CREATE INDEX IF NOT EXISTS idx_roles_ldap_ou_id ON public.roles (ldap_ou_id);

CREATE SEQUENCE IF NOT EXISTS public.ldap_ous_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.ldap_ous (
    id bigint NOT NULL DEFAULT nextval('public.ldap_ous_id_seq'::regclass),
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(64) NOT NULL,
    parent_id bigint,
    CONSTRAINT ldap_ous_pkey PRIMARY KEY (id)
);

ALTER SEQUENCE public.ldap_ous_id_seq OWNED BY public.ldap_ous.id;

CREATE INDEX IF NOT EXISTS idx_ldap_ous_parent_id ON public.ldap_ous (parent_id);

CREATE TABLE IF NOT EXISTS public.role_members (
    role_id bigint NOT NULL,
    member_role_id bigint NOT NULL,
    CONSTRAINT role_members_pkey PRIMARY KEY (role_id, member_role_id)
);

-- +goose Down

DROP TABLE IF EXISTS public.role_members;
DROP TABLE IF EXISTS public.ldap_ous;

ALTER TABLE public.users DROP COLUMN IF EXISTS ldap_ou_id;
ALTER TABLE public.roles DROP COLUMN IF EXISTS ldap_ou_id;

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS ldap_internal_enabled,
    DROP COLUMN IF EXISTS ldap_internal_listen,
    DROP COLUMN IF EXISTS ldap_internal_tls_listen,
    DROP COLUMN IF EXISTS ldap_internal_base_dn,
    DROP COLUMN IF EXISTS ldap_internal_bind_password,
    DROP COLUMN IF EXISTS radius_use_internal_ldap;
