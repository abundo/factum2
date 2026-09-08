-- Code generated from a pg_dump --schema-only of a database after the last
-- AutoMigrate-based MigrateDatabase. Do not hand-edit table lists here;
-- later schema changes are new goose files.
--
-- +goose Up

--
-- PostgreSQL database dump
--

-- Dumped from database version 18.6
-- Dumped by pg_dump version 18.6

--
-- Name: addresses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.addresses (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    address_id bigint,
    interface_id bigint,
    netbox_id bigint,
    address character varying(80),
    vrf character varying(255),
    role character varying(255)
);

--
-- Name: addresses_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.addresses_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: addresses_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.addresses_id_seq OWNED BY public.addresses.id;

--
-- Name: agreements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agreements (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    last_sync bigint,
    monthly_fee bigint,
    onetime_fee bigint
);

--
-- Name: agreements_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.agreements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: agreements_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.agreements_id_seq OWNED BY public.agreements.id;

--
-- Name: config_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.config_assignments (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    variable_def_id bigint NOT NULL,
    scope_id bigint NOT NULL,
    value text
);

--
-- Name: config_assignments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.config_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: config_assignments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.config_assignments_id_seq OWNED BY public.config_assignments.id;

--
-- Name: config_cli_features; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.config_cli_features (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    scope_id bigint NOT NULL,
    name character varying(128) NOT NULL,
    sort_order bigint,
    add_commands text,
    update_commands text,
    remove_commands text,
    remove_at_root boolean
);

--
-- Name: config_cli_features_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.config_cli_features_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: config_cli_features_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.config_cli_features_id_seq OWNED BY public.config_cli_features.id;

--
-- Name: config_macros; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.config_macros (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    body text
);

--
-- Name: config_macros_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.config_macros_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: config_macros_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.config_macros_id_seq OWNED BY public.config_macros.id;

--
-- Name: config_scopes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.config_scopes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    parent_id bigint,
    name character varying(255) NOT NULL,
    kind character varying(32) NOT NULL,
    site_id bigint,
    device_id bigint,
    interface_id bigint,
    service_id bigint,
    service_type_id bigint,
    platform character varying(64),
    payload_kind character varying(32),
    enabled boolean DEFAULT true NOT NULL,
    sort_order bigint,
    payload text,
    seed_checksum character varying(64)
);

--
-- Name: config_scopes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.config_scopes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: config_scopes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.config_scopes_id_seq OWNED BY public.config_scopes.id;

--
-- Name: config_variable_defs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.config_variable_defs (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    type character varying(32) NOT NULL,
    description character varying(255),
    default_value text,
    constraints text,
    secret boolean,
    required boolean,
    platforms text
);

--
-- Name: config_variable_defs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.config_variable_defs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: config_variable_defs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.config_variable_defs_id_seq OWNED BY public.config_variable_defs.id;

--
-- Name: connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connections (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    netbox_id bigint,
    device_a_id bigint,
    interface_a_id bigint,
    device_b_id bigint,
    interface_b_id bigint,
    label character varying(255)
);

--
-- Name: connections_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.connections_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: connections_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.connections_id_seq OWNED BY public.connections.id;

--
-- Name: contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contacts (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    last_sync bigint,
    name text,
    email text,
    phone text,
    notify_maintenance boolean,
    source text,
    source_id text
);

--
-- Name: contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: contacts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.contacts_id_seq OWNED BY public.contacts.id;

--
-- Name: customer_contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.customer_contacts (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    customer_id bigint NOT NULL,
    contact_id bigint NOT NULL
);

--
-- Name: customer_contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.customer_contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: customer_contacts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.customer_contacts_id_seq OWNED BY public.customer_contacts.id;

--
-- Name: customers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.customers (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    last_sync bigint,
    name text,
    postaladdress1 text,
    postaladdress2 text,
    postalcity text,
    postalzipcode text,
    country text,
    organization_number text,
    source text,
    source_id text
);

--
-- Name: customers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.customers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: customers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.customers_id_seq OWNED BY public.customers.id;

--
-- Name: device_sync_auths; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.device_sync_auths (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name text NOT NULL,
    username text,
    password text
);

--
-- Name: device_sync_auths_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.device_sync_auths_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: device_sync_auths_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.device_sync_auths_id_seq OWNED BY public.device_sync_auths.id;

--
-- Name: devices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.devices (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    vm boolean,
    netbox_id bigint,
    name character varying(255),
    comments character varying(255),
    enabled boolean,
    manufacturer character varying(255),
    manufacturer_id bigint,
    model_name character varying(255),
    model_id bigint,
    platform character varying(255),
    platform_id bigint,
    primary_ipv4 character varying(255),
    primary_ipv4_id bigint,
    primary_ipv6 character varying(255),
    primary_ipv6_id bigint,
    role character varying(255),
    role_id bigint,
    site character varying(255),
    site_id bigint,
    status character varying(255),
    latitude numeric,
    longitude numeric,
    optical_kind character varying(32),
    optical_kind_cf character varying(32),
    librenms_id bigint,
    cf_alarm_timeperiod character varying(255),
    cf_alarm_destination character varying(255),
    cf_alarm_interfaces boolean,
    cf_backup_oxidized boolean,
    cf_connection_method character varying(255),
    cf_location character varying(255),
    cf_monitor_grafana boolean,
    cf_monitor_icinga boolean,
    cf_monitor_librenms boolean,
    cf_source character varying(255),
    cf_source_id bigint
);

--
-- Name: devices_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.devices_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: devices_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.devices_id_seq OWNED BY public.devices.id;

--
-- Name: dns_dnssec_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_dnssec_policies (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    ksk_lifetime character varying(64),
    ksk_algorithm character varying(64),
    zsk_lifetime character varying(64),
    zsk_algorithm character varying(64),
    purge_keys character varying(64),
    signatures_validity character varying(64),
    signatures_validity_dnskey character varying(64),
    signatures_refresh character varying(64)
);

--
-- Name: dns_dnssec_policies_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_dnssec_policies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_dnssec_policies_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_dnssec_policies_id_seq OWNED BY public.dns_dnssec_policies.id;

--
-- Name: dns_soa_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_soa_templates (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    mname character varying(255),
    rname character varying(255),
    refresh bigint,
    retry bigint,
    expire bigint,
    ttl bigint
);

--
-- Name: dns_soa_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_soa_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_soa_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_soa_templates_id_seq OWNED BY public.dns_soa_templates.id;

--
-- Name: dns_template_nameservers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_template_nameservers (
    id bigint NOT NULL,
    dns_template_id bigint NOT NULL,
    rank bigint NOT NULL,
    hostname character varying(255) NOT NULL
);

--
-- Name: dns_template_nameservers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_template_nameservers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_template_nameservers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_template_nameservers_id_seq OWNED BY public.dns_template_nameservers.id;

--
-- Name: dns_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_templates (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    soa_template_id bigint,
    default_ttl bigint,
    dns_sec_policy_id bigint
);

--
-- Name: dns_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_templates_id_seq OWNED BY public.dns_templates.id;

--
-- Name: dns_zone_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_zone_records (
    id bigint NOT NULL,
    dns_zone_id bigint NOT NULL,
    rank bigint NOT NULL,
    name character varying(255) NOT NULL,
    ttl bigint,
    record_type character varying(16) NOT NULL,
    value text NOT NULL,
    description text,
    mac character varying(32)
);

--
-- Name: dns_zone_records_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_zone_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_zone_records_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_zone_records_id_seq OWNED BY public.dns_zone_records.id;

--
-- Name: dns_zones; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dns_zones (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    type character varying(16) DEFAULT 'forward'::character varying NOT NULL,
    dns_template_id bigint,
    comment text
);

--
-- Name: dns_zones_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.dns_zones_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: dns_zones_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.dns_zones_id_seq OWNED BY public.dns_zones.id;

--
-- Name: interfaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interfaces (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_id bigint,
    netbox_id bigint,
    name character varying(255),
    description character varying(255),
    enabled boolean,
    vrf character varying(255),
    cf_role character varying(255),
    type character varying(255),
    cable_id bigint,
    label character varying(255),
    parent_id bigint,
    untagged_vlan bigint,
    tagged_vla_ns text,
    vlan_names text,
    switchport_mode character varying(255),
    librenms_id bigint
);

--
-- Name: interfaces_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.interfaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: interfaces_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.interfaces_id_seq OWNED BY public.interfaces.id;

--
-- Name: ipam_namespace_prefixes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ipam_namespace_prefixes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    namespace_id bigint NOT NULL,
    prefix character varying(80) NOT NULL,
    family bigint
);

--
-- Name: ipam_namespace_prefixes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ipam_namespace_prefixes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: ipam_namespace_prefixes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ipam_namespace_prefixes_id_seq OWNED BY public.ipam_namespace_prefixes.id;

--
-- Name: ipam_namespaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ipam_namespaces (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    description character varying(255)
);

--
-- Name: ipam_namespaces_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ipam_namespaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: ipam_namespaces_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ipam_namespaces_id_seq OWNED BY public.ipam_namespaces.id;

--
-- Name: ipam_prefixes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ipam_prefixes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    namespace_id bigint NOT NULL,
    vrf_id bigint NOT NULL,
    prefix character varying(80) NOT NULL,
    family bigint,
    description character varying(255),
    dhcp_enabled boolean,
    dhcp_range_start character varying(80),
    dhcp_range_end character varying(80),
    dhcp_gateway character varying(80),
    dhcp_dns_servers text
);

--
-- Name: ipam_prefixes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ipam_prefixes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: ipam_prefixes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ipam_prefixes_id_seq OWNED BY public.ipam_prefixes.id;

--
-- Name: ipam_vrfs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ipam_vrfs (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    namespace_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    description character varying(255),
    is_default boolean
);

--
-- Name: ipam_vrfs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ipam_vrfs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: ipam_vrfs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ipam_vrfs_id_seq OWNED BY public.ipam_vrfs.id;

--
-- Name: job_schedules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.job_schedules (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name text NOT NULL,
    enabled boolean,
    target text NOT NULL,
    cron text NOT NULL,
    last_run_at timestamp with time zone,
    next_run_at timestamp with time zone,
    last_error text,
    created_by text
);

--
-- Name: job_schedules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.job_schedules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: job_schedules_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.job_schedules_id_seq OWNED BY public.job_schedules.id;

--
-- Name: job_task_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.job_task_events (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    job_task_id bigint,
    task_id text NOT NULL,
    target text,
    level text,
    message text,
    at timestamp with time zone
);

--
-- Name: job_task_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.job_task_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: job_task_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.job_task_events_id_seq OWNED BY public.job_task_events.id;

--
-- Name: job_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.job_tasks (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    job_id bigint NOT NULL,
    task_id text NOT NULL,
    target text,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    exit_code bigint,
    err text,
    error_count bigint DEFAULT 0 NOT NULL,
    warning_count bigint DEFAULT 0 NOT NULL
);

--
-- Name: job_tasks_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.job_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: job_tasks_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.job_tasks_id_seq OWNED BY public.job_tasks.id;

--
-- Name: jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.jobs (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    type text DEFAULT 'sync'::text NOT NULL,
    triggered_by text,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    expected_tasks bigint
);

--
-- Name: jobs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.jobs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: jobs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.jobs_id_seq OWNED BY public.jobs.id;

--
-- Name: ldap_role_mappings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ldap_role_mappings (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    group_dn text NOT NULL,
    role_id bigint NOT NULL
);

--
-- Name: ldap_role_mappings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ldap_role_mappings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: ldap_role_mappings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ldap_role_mappings_id_seq OWNED BY public.ldap_role_mappings.id;

--
-- Name: librenms_pending_deletes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.librenms_pending_deletes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_id bigint NOT NULL,
    hostname text,
    display text,
    reason text,
    scheduled_at timestamp with time zone,
    force_delete boolean
);

--
-- Name: librenms_pending_deletes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.librenms_pending_deletes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: librenms_pending_deletes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.librenms_pending_deletes_id_seq OWNED BY public.librenms_pending_deletes.id;

--
-- Name: links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.links (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    "group" text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    open_in_new_tab boolean,
    icon text,
    "position" bigint
);

--
-- Name: links_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.links_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: links_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.links_id_seq OWNED BY public.links.id;

--
-- Name: maintenance_notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.maintenance_notifications (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    window_id bigint NOT NULL,
    customer_id bigint NOT NULL,
    service_ids text,
    contact_id bigint,
    email text,
    sent_at timestamp with time zone,
    status character varying(16),
    error text
);

--
-- Name: maintenance_notifications_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.maintenance_notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: maintenance_notifications_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.maintenance_notifications_id_seq OWNED BY public.maintenance_notifications.id;

--
-- Name: maintenance_resources; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.maintenance_resources (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    window_id bigint NOT NULL,
    resource_type character varying(16) NOT NULL,
    resource_id bigint NOT NULL
);

--
-- Name: maintenance_resources_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.maintenance_resources_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: maintenance_resources_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.maintenance_resources_id_seq OWNED BY public.maintenance_resources.id;

--
-- Name: maintenance_windows; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.maintenance_windows (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    title character varying(255) NOT NULL,
    description text,
    resource_type character varying(16) NOT NULL,
    resource_id bigint NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone,
    status character varying(16) NOT NULL,
    created_by bigint
);

--
-- Name: maintenance_windows_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.maintenance_windows_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: maintenance_windows_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.maintenance_windows_id_seq OWNED BY public.maintenance_windows.id;

--
-- Name: optical_kind_maps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.optical_kind_maps (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    netbox_role_name character varying(255) NOT NULL,
    optical_kind character varying(32) NOT NULL
);

--
-- Name: optical_kind_maps_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.optical_kind_maps_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: optical_kind_maps_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.optical_kind_maps_id_seq OWNED BY public.optical_kind_maps.id;

--
-- Name: optical_ports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.optical_ports (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    interface_id bigint NOT NULL,
    role character varying(32) NOT NULL,
    freq_hz bigint,
    itu_channel bigint,
    notes character varying(255)
);

--
-- Name: optical_ports_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.optical_ports_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: optical_ports_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.optical_ports_id_seq OWNED BY public.optical_ports.id;

--
-- Name: optical_x_connects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.optical_x_connects (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_id bigint NOT NULL,
    kind character varying(32) NOT NULL,
    interface_a_id bigint NOT NULL,
    interface_b_id bigint NOT NULL,
    freq_hz bigint,
    source character varying(32)
);

--
-- Name: optical_x_connects_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.optical_x_connects_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: optical_x_connects_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.optical_x_connects_id_seq OWNED BY public.optical_x_connects.id;

--
-- Name: password_reset_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.password_reset_tokens (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint NOT NULL,
    token_hash text NOT NULL,
    code_hash text NOT NULL,
    expires_at timestamp with time zone,
    attempts bigint,
    consumed_at timestamp with time zone
);

--
-- Name: password_reset_tokens_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.password_reset_tokens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: password_reset_tokens_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.password_reset_tokens_id_seq OWNED BY public.password_reset_tokens.id;

--
-- Name: products; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.products (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    last_sync bigint,
    name text
);

--
-- Name: products_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.products_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: products_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.products_id_seq OWNED BY public.products.id;

--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name text NOT NULL,
    description text
);

--
-- Name: roles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: roles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.roles_id_seq OWNED BY public.roles.id;

--
-- Name: service_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.service_endpoints (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    service_id bigint NOT NULL,
    role character varying(64) NOT NULL,
    device_id bigint NOT NULL,
    interface_id bigint NOT NULL,
    fields text
);

--
-- Name: service_endpoints_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.service_endpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: service_endpoints_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.service_endpoints_id_seq OWNED BY public.service_endpoints.id;

--
-- Name: service_hops; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.service_hops (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    service_id bigint NOT NULL,
    seq bigint NOT NULL,
    kind character varying(16) NOT NULL,
    interface_id bigint,
    connection_id bigint,
    x_connect_id bigint,
    device_id bigint,
    freq_hz bigint,
    label character varying(255)
);

--
-- Name: service_hops_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.service_hops_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: service_hops_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.service_hops_id_seq OWNED BY public.service_hops.id;

--
-- Name: service_paths; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.service_paths (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    service_id bigint NOT NULL,
    mode character varying(16) NOT NULL,
    status character varying(16) NOT NULL,
    endpoint_a_interface_id bigint NOT NULL,
    endpoint_z_interface_id bigint NOT NULL,
    start_kind_a character varying(32),
    start_kind_z character varying(32),
    freq_hz bigint,
    last_traced_at timestamp with time zone,
    last_trace_error text
);

--
-- Name: service_paths_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.service_paths_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: service_paths_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.service_paths_id_seq OWNED BY public.service_paths.id;

--
-- Name: service_types; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.service_types (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(64) NOT NULL,
    description character varying(255),
    schema text,
    endpoint_roles text,
    builtin boolean,
    sync_source character varying(32),
    netbox_type character varying(32)
);

--
-- Name: service_types_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.service_types_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: service_types_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.service_types_id_seq OWNED BY public.service_types.id;

--
-- Name: services; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.services (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    last_sync bigint,
    name text,
    customer_id bigint,
    comment text,
    service_id text,
    service_type text,
    bandwidth_mbps bigint,
    max_mac_addresses bigint,
    delivery_point1 text,
    delivery_point2 text,
    product text,
    service text,
    agreement_status text,
    endpoint_a_device_id bigint,
    endpoint_a_interface_id bigint,
    endpoint_a_vlan bigint,
    endpoint_a_subinterface_netbox_id bigint,
    endpoint_a_termination_netbox_id bigint,
    endpoint_b_device_id bigint,
    endpoint_b_interface_id bigint,
    endpoint_b_vlan bigint,
    endpoint_b_subinterface_netbox_id bigint,
    endpoint_b_termination_netbox_id bigint,
    applied_endpoint_a_device_id bigint,
    applied_endpoint_a_iface text,
    applied_endpoint_a_vlan bigint,
    applied_endpoint_b_device_id bigint,
    applied_endpoint_b_iface text,
    applied_endpoint_b_vlan bigint,
    pseudowire_id bigint,
    l2_vpn_netbox_id bigint,
    fields text,
    source text,
    source_id text
);

--
-- Name: services_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.services_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: services_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.services_id_seq OWNED BY public.services.id;

--
-- Name: settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.settings (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    becs_enabled boolean,
    dns_enabled boolean,
    icinga_enabled boolean,
    optical_enabled boolean,
    ipam_enabled boolean,
    organization_enabled boolean,
    dns_zones_enabled boolean,
    dhcp_enabled boolean,
    librenms_enabled boolean,
    lime_enabled boolean,
    netbox_enabled boolean,
    oxidized_enabled boolean,
    prometheus_enabled boolean,
    device_sync_enabled boolean,
    factum_api_token text,
    public_base_url text,
    default_domain text,
    job_history_keep bigint,
    becs_eapi_url text,
    becs_eapi_user text,
    becs_eapi_pass text,
    becs_eapi_oid bigint,
    dns_dest_file text,
    dns_ignore_models text,
    dns_ignore_platforms text,
    dns_config_file text,
    dns_db_file text,
    dns_host_template text,
    dns_bind_type text,
    dns_bind_config_dir text,
    dns_bind_include_file text,
    dns_bind_zones_dir text,
    dns_bind_zones_file text,
    dns_bind_tmp_dir text,
    dns_bind_cmd_reload_all text,
    dns_bind_cmd_reload_zone text,
    dns_bind_cmd_restart text,
    dhcp_dns_servers text,
    dhcp_host_template text,
    dhcp_kea_type text,
    dhcp_kea4_config_dir text,
    dhcp_kea4_include_file text,
    dhcp_kea4_tmp_dir text,
    dhcp_kea4_cmd_restart text,
    dhcp_kea6_config_dir text,
    dhcp_kea6_include_file text,
    dhcp_kea6_tmp_dir text,
    dhcp_kea6_cmd_restart text,
    smtp_host text,
    smtp_port integer,
    smtp_user text,
    smtp_pass text,
    smtp_tls_mode text,
    email_sender text,
    icinga_api_url text,
    icinga_api_user text,
    icinga_api_pass text,
    icinga_hosts_file text,
    icinga_users_file text,
    icinga_ignore_devices text,
    icinga_default_notification text,
    icinga_host_template text,
    icinga_dependency_template text,
    icinga_user_template text,
    librenms_api_url text,
    librenms_api_token text,
    librenms_persistent_devices text,
    librenms_delayed_delete_enabled boolean,
    librenms_delayed_delete_days bigint,
    librenms_roles_enabled text,
    librenms_interfaces_disabled text,
    librenms_snmp_version text,
    librenms_snmp_communities text,
    lime_api_url text,
    lime_api_token text,
    netbox_api_url text,
    netbox_api_token text,
    netbox_webhook_secret text,
    netbox_sync_customers_enabled boolean,
    netbox_sync_contacts_enabled boolean,
    oxidized_api_url text,
    oxidized_api_user text,
    oxidized_api_pass text,
    oxidized_dest_file text,
    oxidized_ignore_devices text,
    oxidized_ignore_manufacturers text,
    oxidized_ignore_models text,
    oxidized_ignore_platforms text,
    prometheus_dest_file text,
    prometheus_reload_url text,
    prometheus_module text,
    prometheus_auth text,
    prometheus_ignore_devices text,
    prometheus_ignore_manufacturers text,
    prometheus_ignore_models text,
    prometheus_ignore_platforms text,
    device_sync_vrf_in_global text,
    device_sync_device_states text,
    device_sync_device_ignore text,
    device_sync_vlan_group_name character varying(255),
    ldap_enabled boolean,
    ldap_server_type text,
    ldap_host text,
    ldap_port integer,
    ldap_host2 text,
    ldap_port2 integer,
    ldap_tls_mode text,
    ldap_skip_tls_verify boolean,
    ldap_bind_dn text,
    ldap_bind_password text,
    ldap_base_dn text,
    ldap_user_filter text,
    ldap_attr_username text,
    ldap_attr_email text,
    ldap_attr_display_name text,
    ldap_attr_mobile text,
    ldap_attr_groups text,
    ldap_default_role_id bigint,
    ldap_allow_password_change boolean
);

--
-- Name: settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.settings_id_seq OWNED BY public.settings.id;

--
-- Name: sites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sites (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    netbox_id bigint,
    name character varying(255),
    latitude numeric,
    longitude numeric
);

--
-- Name: sites_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sites_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: sites_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sites_id_seq OWNED BY public.sites.id;

--
-- Name: tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tags (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    device_id bigint,
    interface_id bigint,
    netbox_id bigint,
    name character varying(255)
);

--
-- Name: tags_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.tags_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: tags_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.tags_id_seq OWNED BY public.tags.id;

--
-- Name: user_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_roles (
    role_id bigint NOT NULL,
    user_id bigint NOT NULL
);

--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    username text NOT NULL,
    password_hash text NOT NULL,
    name text NOT NULL,
    email text,
    mobile text
);

--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

--
-- Name: worker_nodes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.worker_nodes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name text NOT NULL,
    address text,
    token text,
    enabled boolean,
    tls_skip_verify boolean,
    tls_ca text
);

--
-- Name: worker_nodes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.worker_nodes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: worker_nodes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.worker_nodes_id_seq OWNED BY public.worker_nodes.id;

--
-- Name: addresses id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addresses ALTER COLUMN id SET DEFAULT nextval('public.addresses_id_seq'::regclass);

--
-- Name: agreements id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agreements ALTER COLUMN id SET DEFAULT nextval('public.agreements_id_seq'::regclass);

--
-- Name: config_assignments id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_assignments ALTER COLUMN id SET DEFAULT nextval('public.config_assignments_id_seq'::regclass);

--
-- Name: config_cli_features id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_cli_features ALTER COLUMN id SET DEFAULT nextval('public.config_cli_features_id_seq'::regclass);

--
-- Name: config_macros id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_macros ALTER COLUMN id SET DEFAULT nextval('public.config_macros_id_seq'::regclass);

--
-- Name: config_scopes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_scopes ALTER COLUMN id SET DEFAULT nextval('public.config_scopes_id_seq'::regclass);

--
-- Name: config_variable_defs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_variable_defs ALTER COLUMN id SET DEFAULT nextval('public.config_variable_defs_id_seq'::regclass);

--
-- Name: connections id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections ALTER COLUMN id SET DEFAULT nextval('public.connections_id_seq'::regclass);

--
-- Name: contacts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts ALTER COLUMN id SET DEFAULT nextval('public.contacts_id_seq'::regclass);

--
-- Name: customer_contacts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customer_contacts ALTER COLUMN id SET DEFAULT nextval('public.customer_contacts_id_seq'::regclass);

--
-- Name: customers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customers ALTER COLUMN id SET DEFAULT nextval('public.customers_id_seq'::regclass);

--
-- Name: device_sync_auths id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.device_sync_auths ALTER COLUMN id SET DEFAULT nextval('public.device_sync_auths_id_seq'::regclass);

--
-- Name: devices id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices ALTER COLUMN id SET DEFAULT nextval('public.devices_id_seq'::regclass);

--
-- Name: dns_dnssec_policies id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_dnssec_policies ALTER COLUMN id SET DEFAULT nextval('public.dns_dnssec_policies_id_seq'::regclass);

--
-- Name: dns_soa_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_soa_templates ALTER COLUMN id SET DEFAULT nextval('public.dns_soa_templates_id_seq'::regclass);

--
-- Name: dns_template_nameservers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_template_nameservers ALTER COLUMN id SET DEFAULT nextval('public.dns_template_nameservers_id_seq'::regclass);

--
-- Name: dns_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_templates ALTER COLUMN id SET DEFAULT nextval('public.dns_templates_id_seq'::regclass);

--
-- Name: dns_zone_records id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zone_records ALTER COLUMN id SET DEFAULT nextval('public.dns_zone_records_id_seq'::regclass);

--
-- Name: dns_zones id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zones ALTER COLUMN id SET DEFAULT nextval('public.dns_zones_id_seq'::regclass);

--
-- Name: interfaces id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interfaces ALTER COLUMN id SET DEFAULT nextval('public.interfaces_id_seq'::regclass);

--
-- Name: ipam_namespace_prefixes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_namespace_prefixes ALTER COLUMN id SET DEFAULT nextval('public.ipam_namespace_prefixes_id_seq'::regclass);

--
-- Name: ipam_namespaces id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_namespaces ALTER COLUMN id SET DEFAULT nextval('public.ipam_namespaces_id_seq'::regclass);

--
-- Name: ipam_prefixes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_prefixes ALTER COLUMN id SET DEFAULT nextval('public.ipam_prefixes_id_seq'::regclass);

--
-- Name: ipam_vrfs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_vrfs ALTER COLUMN id SET DEFAULT nextval('public.ipam_vrfs_id_seq'::regclass);

--
-- Name: job_schedules id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_schedules ALTER COLUMN id SET DEFAULT nextval('public.job_schedules_id_seq'::regclass);

--
-- Name: job_task_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_task_events ALTER COLUMN id SET DEFAULT nextval('public.job_task_events_id_seq'::regclass);

--
-- Name: job_tasks id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_tasks ALTER COLUMN id SET DEFAULT nextval('public.job_tasks_id_seq'::regclass);

--
-- Name: jobs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.jobs ALTER COLUMN id SET DEFAULT nextval('public.jobs_id_seq'::regclass);

--
-- Name: ldap_role_mappings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ldap_role_mappings ALTER COLUMN id SET DEFAULT nextval('public.ldap_role_mappings_id_seq'::regclass);

--
-- Name: librenms_pending_deletes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.librenms_pending_deletes ALTER COLUMN id SET DEFAULT nextval('public.librenms_pending_deletes_id_seq'::regclass);

--
-- Name: links id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.links ALTER COLUMN id SET DEFAULT nextval('public.links_id_seq'::regclass);

--
-- Name: maintenance_notifications id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_notifications ALTER COLUMN id SET DEFAULT nextval('public.maintenance_notifications_id_seq'::regclass);

--
-- Name: maintenance_resources id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_resources ALTER COLUMN id SET DEFAULT nextval('public.maintenance_resources_id_seq'::regclass);

--
-- Name: maintenance_windows id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_windows ALTER COLUMN id SET DEFAULT nextval('public.maintenance_windows_id_seq'::regclass);

--
-- Name: optical_kind_maps id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_kind_maps ALTER COLUMN id SET DEFAULT nextval('public.optical_kind_maps_id_seq'::regclass);

--
-- Name: optical_ports id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_ports ALTER COLUMN id SET DEFAULT nextval('public.optical_ports_id_seq'::regclass);

--
-- Name: optical_x_connects id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_x_connects ALTER COLUMN id SET DEFAULT nextval('public.optical_x_connects_id_seq'::regclass);

--
-- Name: password_reset_tokens id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.password_reset_tokens ALTER COLUMN id SET DEFAULT nextval('public.password_reset_tokens_id_seq'::regclass);

--
-- Name: products id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products ALTER COLUMN id SET DEFAULT nextval('public.products_id_seq'::regclass);

--
-- Name: roles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles ALTER COLUMN id SET DEFAULT nextval('public.roles_id_seq'::regclass);

--
-- Name: service_endpoints id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_endpoints ALTER COLUMN id SET DEFAULT nextval('public.service_endpoints_id_seq'::regclass);

--
-- Name: service_hops id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_hops ALTER COLUMN id SET DEFAULT nextval('public.service_hops_id_seq'::regclass);

--
-- Name: service_paths id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_paths ALTER COLUMN id SET DEFAULT nextval('public.service_paths_id_seq'::regclass);

--
-- Name: service_types id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_types ALTER COLUMN id SET DEFAULT nextval('public.service_types_id_seq'::regclass);

--
-- Name: services id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.services ALTER COLUMN id SET DEFAULT nextval('public.services_id_seq'::regclass);

--
-- Name: settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.settings ALTER COLUMN id SET DEFAULT nextval('public.settings_id_seq'::regclass);

--
-- Name: sites id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sites ALTER COLUMN id SET DEFAULT nextval('public.sites_id_seq'::regclass);

--
-- Name: tags id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags ALTER COLUMN id SET DEFAULT nextval('public.tags_id_seq'::regclass);

--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

--
-- Name: worker_nodes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_nodes ALTER COLUMN id SET DEFAULT nextval('public.worker_nodes_id_seq'::regclass);

--
-- Name: addresses addresses_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addresses
    ADD CONSTRAINT addresses_pkey PRIMARY KEY (id);

--
-- Name: agreements agreements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agreements
    ADD CONSTRAINT agreements_pkey PRIMARY KEY (id);

--
-- Name: config_assignments config_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_assignments
    ADD CONSTRAINT config_assignments_pkey PRIMARY KEY (id);

--
-- Name: config_cli_features config_cli_features_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_cli_features
    ADD CONSTRAINT config_cli_features_pkey PRIMARY KEY (id);

--
-- Name: config_macros config_macros_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_macros
    ADD CONSTRAINT config_macros_pkey PRIMARY KEY (id);

--
-- Name: config_scopes config_scopes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_scopes
    ADD CONSTRAINT config_scopes_pkey PRIMARY KEY (id);

--
-- Name: config_variable_defs config_variable_defs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.config_variable_defs
    ADD CONSTRAINT config_variable_defs_pkey PRIMARY KEY (id);

--
-- Name: connections connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_pkey PRIMARY KEY (id);

--
-- Name: contacts contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_pkey PRIMARY KEY (id);

--
-- Name: customer_contacts customer_contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customer_contacts
    ADD CONSTRAINT customer_contacts_pkey PRIMARY KEY (id);

--
-- Name: customers customers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_pkey PRIMARY KEY (id);

--
-- Name: device_sync_auths device_sync_auths_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.device_sync_auths
    ADD CONSTRAINT device_sync_auths_pkey PRIMARY KEY (id);

--
-- Name: devices devices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_pkey PRIMARY KEY (id);

--
-- Name: dns_dnssec_policies dns_dnssec_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_dnssec_policies
    ADD CONSTRAINT dns_dnssec_policies_pkey PRIMARY KEY (id);

--
-- Name: dns_soa_templates dns_soa_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_soa_templates
    ADD CONSTRAINT dns_soa_templates_pkey PRIMARY KEY (id);

--
-- Name: dns_template_nameservers dns_template_nameservers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_template_nameservers
    ADD CONSTRAINT dns_template_nameservers_pkey PRIMARY KEY (id);

--
-- Name: dns_templates dns_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_templates
    ADD CONSTRAINT dns_templates_pkey PRIMARY KEY (id);

--
-- Name: dns_zone_records dns_zone_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zone_records
    ADD CONSTRAINT dns_zone_records_pkey PRIMARY KEY (id);

--
-- Name: dns_zones dns_zones_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zones
    ADD CONSTRAINT dns_zones_pkey PRIMARY KEY (id);

--
-- Name: interfaces interfaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interfaces
    ADD CONSTRAINT interfaces_pkey PRIMARY KEY (id);

--
-- Name: ipam_namespace_prefixes ipam_namespace_prefixes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_namespace_prefixes
    ADD CONSTRAINT ipam_namespace_prefixes_pkey PRIMARY KEY (id);

--
-- Name: ipam_namespaces ipam_namespaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_namespaces
    ADD CONSTRAINT ipam_namespaces_pkey PRIMARY KEY (id);

--
-- Name: ipam_prefixes ipam_prefixes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_prefixes
    ADD CONSTRAINT ipam_prefixes_pkey PRIMARY KEY (id);

--
-- Name: ipam_vrfs ipam_vrfs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ipam_vrfs
    ADD CONSTRAINT ipam_vrfs_pkey PRIMARY KEY (id);

--
-- Name: job_schedules job_schedules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_schedules
    ADD CONSTRAINT job_schedules_pkey PRIMARY KEY (id);

--
-- Name: job_task_events job_task_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_task_events
    ADD CONSTRAINT job_task_events_pkey PRIMARY KEY (id);

--
-- Name: job_tasks job_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_tasks
    ADD CONSTRAINT job_tasks_pkey PRIMARY KEY (id);

--
-- Name: jobs jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_pkey PRIMARY KEY (id);

--
-- Name: ldap_role_mappings ldap_role_mappings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ldap_role_mappings
    ADD CONSTRAINT ldap_role_mappings_pkey PRIMARY KEY (id);

--
-- Name: librenms_pending_deletes librenms_pending_deletes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.librenms_pending_deletes
    ADD CONSTRAINT librenms_pending_deletes_pkey PRIMARY KEY (id);

--
-- Name: links links_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.links
    ADD CONSTRAINT links_pkey PRIMARY KEY (id);

--
-- Name: maintenance_notifications maintenance_notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_notifications
    ADD CONSTRAINT maintenance_notifications_pkey PRIMARY KEY (id);

--
-- Name: maintenance_resources maintenance_resources_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_resources
    ADD CONSTRAINT maintenance_resources_pkey PRIMARY KEY (id);

--
-- Name: maintenance_windows maintenance_windows_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_windows
    ADD CONSTRAINT maintenance_windows_pkey PRIMARY KEY (id);

--
-- Name: optical_kind_maps optical_kind_maps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_kind_maps
    ADD CONSTRAINT optical_kind_maps_pkey PRIMARY KEY (id);

--
-- Name: optical_ports optical_ports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_ports
    ADD CONSTRAINT optical_ports_pkey PRIMARY KEY (id);

--
-- Name: optical_x_connects optical_x_connects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.optical_x_connects
    ADD CONSTRAINT optical_x_connects_pkey PRIMARY KEY (id);

--
-- Name: password_reset_tokens password_reset_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.password_reset_tokens
    ADD CONSTRAINT password_reset_tokens_pkey PRIMARY KEY (id);

--
-- Name: products products_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_pkey PRIMARY KEY (id);

--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

--
-- Name: service_endpoints service_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_endpoints
    ADD CONSTRAINT service_endpoints_pkey PRIMARY KEY (id);

--
-- Name: service_hops service_hops_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_hops
    ADD CONSTRAINT service_hops_pkey PRIMARY KEY (id);

--
-- Name: service_paths service_paths_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_paths
    ADD CONSTRAINT service_paths_pkey PRIMARY KEY (id);

--
-- Name: service_types service_types_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_types
    ADD CONSTRAINT service_types_pkey PRIMARY KEY (id);

--
-- Name: services services_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.services
    ADD CONSTRAINT services_pkey PRIMARY KEY (id);

--
-- Name: settings settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (id);

--
-- Name: sites sites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sites
    ADD CONSTRAINT sites_pkey PRIMARY KEY (id);

--
-- Name: tags tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT tags_pkey PRIMARY KEY (id);

--
-- Name: user_roles user_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_pkey PRIMARY KEY (role_id, user_id);

--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

--
-- Name: worker_nodes worker_nodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_nodes
    ADD CONSTRAINT worker_nodes_pkey PRIMARY KEY (id);

--
-- Name: idx_addresses_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_addresses_interface_id ON public.addresses USING btree (interface_id);

--
-- Name: idx_cfg_assign_var_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_cfg_assign_var_scope ON public.config_assignments USING btree (variable_def_id, scope_id);

--
-- Name: idx_cfg_feat_scope_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_cfg_feat_scope_name ON public.config_cli_features USING btree (scope_id, name);

--
-- Name: idx_config_macros_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_macros_name ON public.config_macros USING btree (name);

--
-- Name: idx_config_scopes_cli_type_plat; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_scopes_cli_type_plat ON public.config_scopes USING btree (service_type_id, platform) WHERE (((kind)::text = 'cli'::text) AND (service_type_id IS NOT NULL));

--
-- Name: idx_config_scopes_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_config_scopes_device_id ON public.config_scopes USING btree (device_id);

--
-- Name: idx_config_scopes_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_config_scopes_interface_id ON public.config_scopes USING btree (interface_id);

--
-- Name: idx_config_scopes_one_device; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_scopes_one_device ON public.config_scopes USING btree (device_id) WHERE (((kind)::text = 'device'::text) AND (device_id IS NOT NULL));

--
-- Name: idx_config_scopes_one_interface; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_scopes_one_interface ON public.config_scopes USING btree (interface_id) WHERE (((kind)::text = 'interface'::text) AND (interface_id IS NOT NULL));

--
-- Name: idx_config_scopes_one_service; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_scopes_one_service ON public.config_scopes USING btree (service_id) WHERE (((kind)::text = 'service'::text) AND (service_id IS NOT NULL));

--
-- Name: idx_config_scopes_service_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_config_scopes_service_id ON public.config_scopes USING btree (service_id);

--
-- Name: idx_config_scopes_service_type_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_config_scopes_service_type_id ON public.config_scopes USING btree (service_type_id);

--
-- Name: idx_config_scopes_site_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_config_scopes_site_id ON public.config_scopes USING btree (site_id);

--
-- Name: idx_config_variable_defs_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_config_variable_defs_name ON public.config_variable_defs USING btree (name);

--
-- Name: idx_connections_device_a_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_device_a_id ON public.connections USING btree (device_a_id);

--
-- Name: idx_connections_device_b_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_device_b_id ON public.connections USING btree (device_b_id);

--
-- Name: idx_connections_interface_a_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_interface_a_id ON public.connections USING btree (interface_a_id);

--
-- Name: idx_connections_interface_b_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_interface_b_id ON public.connections USING btree (interface_b_id);

--
-- Name: idx_connections_netbox_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_connections_netbox_id ON public.connections USING btree (netbox_id);

--
-- Name: idx_contacts_lime_source_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_contacts_lime_source_id ON public.contacts USING btree (source, source_id) WHERE ((source = 'lime'::text) AND (source_id IS NOT NULL) AND (source_id <> ''::text));

--
-- Name: idx_customer_contacts; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_customer_contacts ON public.customer_contacts USING btree (customer_id, contact_id);

--
-- Name: idx_customer_contacts_contact_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customer_contacts_contact_id ON public.customer_contacts USING btree (contact_id);

--
-- Name: idx_customers_lime_source_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_customers_lime_source_id ON public.customers USING btree (source, source_id) WHERE ((source = 'lime'::text) AND (source_id IS NOT NULL) AND (source_id <> ''::text));

--
-- Name: idx_device_sync_auths_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_device_sync_auths_name ON public.device_sync_auths USING btree (name);

--
-- Name: idx_devices_netbox_id_vm; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_devices_netbox_id_vm ON public.devices USING btree (vm, netbox_id);

--
-- Name: idx_devices_optical_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_optical_kind ON public.devices USING btree (optical_kind);

--
-- Name: idx_dns_dnssec_policies_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_dnssec_policies_name ON public.dns_dnssec_policies USING btree (name);

--
-- Name: idx_dns_soa_templates_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_soa_templates_name ON public.dns_soa_templates USING btree (name);

--
-- Name: idx_dns_template_ns_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_template_ns_rank ON public.dns_template_nameservers USING btree (dns_template_id, rank);

--
-- Name: idx_dns_templates_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_templates_name ON public.dns_templates USING btree (name);

--
-- Name: idx_dns_zone_record_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_zone_record_rank ON public.dns_zone_records USING btree (dns_zone_id, rank);

--
-- Name: idx_dns_zones_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dns_zones_name ON public.dns_zones USING btree (name);

--
-- Name: idx_interfaces_device_id_netbox_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_interfaces_device_id_netbox_id ON public.interfaces USING btree (device_id, netbox_id);

--
-- Name: idx_ipam_alloc_ns_pfx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ipam_alloc_ns_pfx ON public.ipam_prefixes USING btree (namespace_id, prefix);

--
-- Name: idx_ipam_namespaces_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ipam_namespaces_name ON public.ipam_namespaces USING btree (name);

--
-- Name: idx_ipam_ns_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ipam_ns_pool ON public.ipam_namespace_prefixes USING btree (namespace_id, prefix);

--
-- Name: idx_ipam_prefixes_vrf_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ipam_prefixes_vrf_id ON public.ipam_prefixes USING btree (vrf_id);

--
-- Name: idx_ipam_vrf_ns_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ipam_vrf_ns_name ON public.ipam_vrfs USING btree (namespace_id, name);

--
-- Name: idx_job_schedules_next_run_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_schedules_next_run_at ON public.job_schedules USING btree (next_run_at);

--
-- Name: idx_job_schedules_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_schedules_target ON public.job_schedules USING btree (target);

--
-- Name: idx_job_task_events_job_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_task_events_job_task_id ON public.job_task_events USING btree (job_task_id);

--
-- Name: idx_job_task_events_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_task_events_task_id ON public.job_task_events USING btree (task_id);

--
-- Name: idx_job_tasks_job_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_tasks_job_id ON public.job_tasks USING btree (job_id);

--
-- Name: idx_job_tasks_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_job_tasks_target ON public.job_tasks USING btree (target);

--
-- Name: idx_job_tasks_task_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_job_tasks_task_id ON public.job_tasks USING btree (task_id);

--
-- Name: idx_jobs_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_jobs_type ON public.jobs USING btree (type);

--
-- Name: idx_ldap_role_mappings_group_dn; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ldap_role_mappings_group_dn ON public.ldap_role_mappings USING btree (group_dn);

--
-- Name: idx_librenms_pending_deletes_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_librenms_pending_deletes_device_id ON public.librenms_pending_deletes USING btree (device_id);

--
-- Name: idx_links_group; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_links_group ON public.links USING btree ("group");

--
-- Name: idx_maint_resources; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_maint_resources ON public.maintenance_resources USING btree (window_id, resource_type, resource_id);

--
-- Name: idx_maintenance_notifications_customer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_notifications_customer_id ON public.maintenance_notifications USING btree (customer_id);

--
-- Name: idx_maintenance_notifications_window_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_notifications_window_id ON public.maintenance_notifications USING btree (window_id);

--
-- Name: idx_maintenance_resources_window_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_resources_window_id ON public.maintenance_resources USING btree (window_id);

--
-- Name: idx_maintenance_windows_resource_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_windows_resource_id ON public.maintenance_windows USING btree (resource_id);

--
-- Name: idx_maintenance_windows_resource_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_windows_resource_type ON public.maintenance_windows USING btree (resource_type);

--
-- Name: idx_maintenance_windows_starts_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_windows_starts_at ON public.maintenance_windows USING btree (starts_at);

--
-- Name: idx_maintenance_windows_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_maintenance_windows_status ON public.maintenance_windows USING btree (status);

--
-- Name: idx_optical_kind_maps_netbox_role_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_optical_kind_maps_netbox_role_name ON public.optical_kind_maps USING btree (netbox_role_name);

--
-- Name: idx_optical_ports_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_optical_ports_interface_id ON public.optical_ports USING btree (interface_id);

--
-- Name: idx_optical_ports_role; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_ports_role ON public.optical_ports USING btree (role);

--
-- Name: idx_optical_x_connects_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_x_connects_device_id ON public.optical_x_connects USING btree (device_id);

--
-- Name: idx_optical_x_connects_interface_a_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_x_connects_interface_a_id ON public.optical_x_connects USING btree (interface_a_id);

--
-- Name: idx_optical_x_connects_interface_b_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_x_connects_interface_b_id ON public.optical_x_connects USING btree (interface_b_id);

--
-- Name: idx_optical_x_connects_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_x_connects_kind ON public.optical_x_connects USING btree (kind);

--
-- Name: idx_optical_x_connects_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_optical_x_connects_source ON public.optical_x_connects USING btree (source);

--
-- Name: idx_password_reset_tokens_token_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_password_reset_tokens_token_hash ON public.password_reset_tokens USING btree (token_hash);

--
-- Name: idx_password_reset_tokens_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_password_reset_tokens_user_id ON public.password_reset_tokens USING btree (user_id);

--
-- Name: idx_roles_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_roles_name ON public.roles USING btree (name);

--
-- Name: idx_service_endpoints_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_endpoints_device_id ON public.service_endpoints USING btree (device_id);

--
-- Name: idx_service_endpoints_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_endpoints_interface_id ON public.service_endpoints USING btree (interface_id);

--
-- Name: idx_service_endpoints_service_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_endpoints_service_id ON public.service_endpoints USING btree (service_id);

--
-- Name: idx_service_hops_connection_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_connection_id ON public.service_hops USING btree (connection_id);

--
-- Name: idx_service_hops_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_device_id ON public.service_hops USING btree (device_id);

--
-- Name: idx_service_hops_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_interface_id ON public.service_hops USING btree (interface_id);

--
-- Name: idx_service_hops_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_kind ON public.service_hops USING btree (kind);

--
-- Name: idx_service_hops_service_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_service_id ON public.service_hops USING btree (service_id);

--
-- Name: idx_service_hops_x_connect_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_hops_x_connect_id ON public.service_hops USING btree (x_connect_id);

--
-- Name: idx_service_paths_endpoint_a_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_paths_endpoint_a_interface_id ON public.service_paths USING btree (endpoint_a_interface_id);

--
-- Name: idx_service_paths_endpoint_z_interface_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_paths_endpoint_z_interface_id ON public.service_paths USING btree (endpoint_z_interface_id);

--
-- Name: idx_service_paths_service_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_service_paths_service_id ON public.service_paths USING btree (service_id);

--
-- Name: idx_service_paths_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_service_paths_status ON public.service_paths USING btree (status);

--
-- Name: idx_service_types_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_service_types_name ON public.service_types USING btree (name);

--
-- Name: idx_services_lime_source_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_services_lime_source_id ON public.services USING btree (source, source_id) WHERE ((source = 'lime'::text) AND (source_id IS NOT NULL) AND (source_id <> ''::text));

--
-- Name: idx_sites_netbox_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_sites_netbox_id ON public.sites USING btree (netbox_id);

--
-- Name: idx_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (username);

--
-- Name: idx_worker_nodes_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_worker_nodes_name ON public.worker_nodes USING btree (name);

--
-- Name: interfaces fk_devices_interfaces; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interfaces
    ADD CONSTRAINT fk_devices_interfaces FOREIGN KEY (device_id) REFERENCES public.devices(id);

--
-- Name: tags fk_devices_tags; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT fk_devices_tags FOREIGN KEY (device_id) REFERENCES public.devices(id);

--
-- Name: dns_templates fk_dns_templates_dns_sec_policy; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_templates
    ADD CONSTRAINT fk_dns_templates_dns_sec_policy FOREIGN KEY (dns_sec_policy_id) REFERENCES public.dns_dnssec_policies(id) ON UPDATE CASCADE ON DELETE RESTRICT;

--
-- Name: dns_template_nameservers fk_dns_templates_nameservers; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_template_nameservers
    ADD CONSTRAINT fk_dns_templates_nameservers FOREIGN KEY (dns_template_id) REFERENCES public.dns_templates(id) ON UPDATE CASCADE ON DELETE CASCADE;

--
-- Name: dns_templates fk_dns_templates_soa_template; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_templates
    ADD CONSTRAINT fk_dns_templates_soa_template FOREIGN KEY (soa_template_id) REFERENCES public.dns_soa_templates(id) ON UPDATE CASCADE ON DELETE RESTRICT;

--
-- Name: dns_zones fk_dns_zones_dns_template; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zones
    ADD CONSTRAINT fk_dns_zones_dns_template FOREIGN KEY (dns_template_id) REFERENCES public.dns_templates(id) ON UPDATE CASCADE ON DELETE RESTRICT;

--
-- Name: dns_zone_records fk_dns_zones_records; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dns_zone_records
    ADD CONSTRAINT fk_dns_zones_records FOREIGN KEY (dns_zone_id) REFERENCES public.dns_zones(id) ON UPDATE CASCADE ON DELETE CASCADE;

--
-- Name: addresses fk_interfaces_addresses; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addresses
    ADD CONSTRAINT fk_interfaces_addresses FOREIGN KEY (interface_id) REFERENCES public.interfaces(id);

--
-- Name: tags fk_interfaces_tags; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT fk_interfaces_tags FOREIGN KEY (interface_id) REFERENCES public.interfaces(id);

--
-- Name: job_tasks fk_jobs_tasks; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.job_tasks
    ADD CONSTRAINT fk_jobs_tasks FOREIGN KEY (job_id) REFERENCES public.jobs(id);

--
-- Name: maintenance_resources fk_maintenance_windows_resources; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.maintenance_resources
    ADD CONSTRAINT fk_maintenance_windows_resources FOREIGN KEY (window_id) REFERENCES public.maintenance_windows(id);

--
-- Name: service_hops fk_service_paths_hops; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.service_hops
    ADD CONSTRAINT fk_service_paths_hops FOREIGN KEY (service_id) REFERENCES public.service_paths(service_id);

--
-- Name: user_roles fk_user_roles_role; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES public.roles(id);

--
-- Name: user_roles fk_user_roles_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES public.users(id);

--
-- PostgreSQL database dump complete
--

-- +goose Down
-- Baseline is not reversible; restore from backup.
