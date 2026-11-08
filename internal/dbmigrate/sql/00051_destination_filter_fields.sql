-- Per-destination copies of the Common ignore lists, plus Roles enabled.
-- An empty destination list uses the matching common_* column.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS common_roles_enabled text,
    ADD COLUMN IF NOT EXISTS dns_ignore_devices text,
    ADD COLUMN IF NOT EXISTS dns_ignore_manufacturers text,
    ADD COLUMN IF NOT EXISTS dns_ignore_interfaces text,
    ADD COLUMN IF NOT EXISTS dns_roles_enabled text,
    ADD COLUMN IF NOT EXISTS icinga_ignore_manufacturers text,
    ADD COLUMN IF NOT EXISTS icinga_ignore_models text,
    ADD COLUMN IF NOT EXISTS icinga_ignore_platforms text,
    ADD COLUMN IF NOT EXISTS icinga_ignore_interfaces text,
    ADD COLUMN IF NOT EXISTS icinga_roles_enabled text,
    ADD COLUMN IF NOT EXISTS librenms_ignore_devices text,
    ADD COLUMN IF NOT EXISTS librenms_ignore_manufacturers text,
    ADD COLUMN IF NOT EXISTS librenms_ignore_models text,
    ADD COLUMN IF NOT EXISTS librenms_ignore_platforms text,
    ADD COLUMN IF NOT EXISTS librenms_ignore_interfaces text,
    ADD COLUMN IF NOT EXISTS oxidized_ignore_interfaces text,
    ADD COLUMN IF NOT EXISTS oxidized_roles_enabled text,
    ADD COLUMN IF NOT EXISTS prometheus_roles_enabled text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS prometheus_roles_enabled,
    DROP COLUMN IF EXISTS oxidized_roles_enabled,
    DROP COLUMN IF EXISTS oxidized_ignore_interfaces,
    DROP COLUMN IF EXISTS librenms_ignore_interfaces,
    DROP COLUMN IF EXISTS librenms_ignore_platforms,
    DROP COLUMN IF EXISTS librenms_ignore_models,
    DROP COLUMN IF EXISTS librenms_ignore_manufacturers,
    DROP COLUMN IF EXISTS librenms_ignore_devices,
    DROP COLUMN IF EXISTS icinga_roles_enabled,
    DROP COLUMN IF EXISTS icinga_ignore_interfaces,
    DROP COLUMN IF EXISTS icinga_ignore_platforms,
    DROP COLUMN IF EXISTS icinga_ignore_models,
    DROP COLUMN IF EXISTS icinga_ignore_manufacturers,
    DROP COLUMN IF EXISTS dns_roles_enabled,
    DROP COLUMN IF EXISTS dns_ignore_interfaces,
    DROP COLUMN IF EXISTS dns_ignore_manufacturers,
    DROP COLUMN IF EXISTS dns_ignore_devices,
    DROP COLUMN IF EXISTS common_roles_enabled;
