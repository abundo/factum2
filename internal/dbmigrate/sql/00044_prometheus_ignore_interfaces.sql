-- Prometheus "Ignore interfaces": newline-separated regular expressions.
-- Matching ifName, ifDescr, or ifAlias samples are dropped at scrape time.
--
-- +goose Up

ALTER TABLE public.settings
    ADD COLUMN IF NOT EXISTS prometheus_ignore_interfaces text;

-- +goose Down

ALTER TABLE public.settings
    DROP COLUMN IF EXISTS prometheus_ignore_interfaces;
