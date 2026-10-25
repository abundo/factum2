-- External OpenLDAP and Active Directory servers. Login tries enabled
-- rows in position order. The Settings LDAP columns stay so an older
-- reader still sees the first server; new edits go through ldap_servers.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS public.ldap_servers (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    position integer NOT NULL DEFAULT 0,
    enabled boolean,
    name text,
    server_type text,
    host text,
    port integer,
    tls_mode text,
    skip_tls_verify boolean,
    bind_dn text,
    bind_password text,
    base_dn text,
    user_filter text,
    attr_username text,
    attr_email text,
    attr_display_name text,
    attr_mobile text,
    attr_groups text,
    allow_password_change boolean
);

INSERT INTO public.ldap_servers (
    created_at, updated_at, position, enabled, name, server_type, host, port,
    tls_mode, skip_tls_verify, bind_dn, bind_password, base_dn, user_filter,
    attr_username, attr_email, attr_display_name, attr_mobile, attr_groups,
    allow_password_change
)
SELECT
    now(), now(), 1,
    s.ldap_enabled,
    '',
    COALESCE(NULLIF(btrim(s.ldap_server_type), ''), 'generic'),
    btrim(s.ldap_host),
    COALESCE(NULLIF(s.ldap_port, 0), 389),
    COALESCE(NULLIF(btrim(s.ldap_tls_mode), ''), 'none'),
    s.ldap_skip_tls_verify,
    s.ldap_bind_dn,
    s.ldap_bind_password,
    s.ldap_base_dn,
    s.ldap_user_filter,
    s.ldap_attr_username,
    s.ldap_attr_email,
    s.ldap_attr_display_name,
    s.ldap_attr_mobile,
    s.ldap_attr_groups,
    s.ldap_allow_password_change
FROM public.settings s
WHERE COALESCE(btrim(s.ldap_host), '') <> ''
  AND NOT EXISTS (SELECT 1 FROM public.ldap_servers);

INSERT INTO public.ldap_servers (
    created_at, updated_at, position, enabled, name, server_type, host, port,
    tls_mode, skip_tls_verify, bind_dn, bind_password, base_dn, user_filter,
    attr_username, attr_email, attr_display_name, attr_mobile, attr_groups,
    allow_password_change
)
SELECT
    now(), now(), 2,
    s.ldap_enabled,
    '',
    COALESCE(NULLIF(btrim(s.ldap_server_type), ''), 'generic'),
    btrim(s.ldap_host2),
    CASE
        WHEN COALESCE(s.ldap_port2, 0) = 0 THEN COALESCE(NULLIF(s.ldap_port, 0), 389)
        ELSE s.ldap_port2
    END,
    COALESCE(NULLIF(btrim(s.ldap_tls_mode), ''), 'none'),
    s.ldap_skip_tls_verify,
    s.ldap_bind_dn,
    s.ldap_bind_password,
    s.ldap_base_dn,
    s.ldap_user_filter,
    s.ldap_attr_username,
    s.ldap_attr_email,
    s.ldap_attr_display_name,
    s.ldap_attr_mobile,
    s.ldap_attr_groups,
    s.ldap_allow_password_change
FROM public.settings s
WHERE COALESCE(btrim(s.ldap_host2), '') <> ''
  AND NOT (
      lower(btrim(s.ldap_host2)) = lower(btrim(COALESCE(s.ldap_host, '')))
      AND (
          CASE
              WHEN COALESCE(s.ldap_port2, 0) = 0 THEN COALESCE(NULLIF(s.ldap_port, 0), 389)
              ELSE s.ldap_port2
          END
      ) = COALESCE(NULLIF(s.ldap_port, 0), 389)
  )
  AND NOT EXISTS (
      SELECT 1 FROM public.ldap_servers x
      WHERE lower(x.host) = lower(btrim(s.ldap_host2))
        AND x.port = CASE
            WHEN COALESCE(s.ldap_port2, 0) = 0 THEN COALESCE(NULLIF(s.ldap_port, 0), 389)
            ELSE s.ldap_port2
        END
  );

-- +goose Down

DROP TABLE IF EXISTS public.ldap_servers;
