-- Destination file settings store a worker.paths key. The filesystem
-- path stays in each worker's factum2-worker.yaml. Any previous path in
-- these columns was accepted only when it matched that same key's pin,
-- so rewriting a non-empty value to the key keeps the worker writing the
-- file it already had configured.
--
-- +goose Up

UPDATE public.settings SET dns_dest_file = 'dns_dest_file'
    WHERE dns_dest_file IS NOT NULL AND btrim(dns_dest_file) <> '';
UPDATE public.settings SET dns_zones_file = 'dns_zones_file'
    WHERE dns_zones_file IS NOT NULL AND btrim(dns_zones_file) <> '';
UPDATE public.settings SET dhcp_prefixes_file = 'dns_prefixes_file'
    WHERE dhcp_prefixes_file IS NOT NULL AND btrim(dhcp_prefixes_file) <> '';
UPDATE public.settings SET certs_lego_yaml = 'lego_yaml'
    WHERE certs_lego_yaml IS NOT NULL AND btrim(certs_lego_yaml) <> '';
UPDATE public.settings SET certs_env_file = 'lego_env'
    WHERE certs_env_file IS NOT NULL AND btrim(certs_env_file) <> '';
UPDATE public.settings SET certs_lego_bin = 'lego_bin'
    WHERE certs_lego_bin IS NOT NULL AND btrim(certs_lego_bin) <> '';
UPDATE public.settings SET certs_lego_storage = 'lego_storage'
    WHERE certs_lego_storage IS NOT NULL AND btrim(certs_lego_storage) <> '';
UPDATE public.settings SET icinga_hosts_file = 'icinga_hosts_file'
    WHERE icinga_hosts_file IS NOT NULL AND btrim(icinga_hosts_file) <> '';
UPDATE public.settings SET icinga_users_file = 'icinga_users_file'
    WHERE icinga_users_file IS NOT NULL AND btrim(icinga_users_file) <> '';
UPDATE public.settings SET icinga_certs_file = 'icinga_certs_file'
    WHERE icinga_certs_file IS NOT NULL AND btrim(icinga_certs_file) <> '';
UPDATE public.settings SET oxidized_dest_file = 'oxidized_dest_file'
    WHERE oxidized_dest_file IS NOT NULL AND btrim(oxidized_dest_file) <> '';
UPDATE public.settings SET prometheus_dest_file = 'prometheus_dest_file'
    WHERE prometheus_dest_file IS NOT NULL AND btrim(prometheus_dest_file) <> '';

-- +goose Down

-- The old filesystem paths are not stored. Leave the keys in place.
SELECT 1;
