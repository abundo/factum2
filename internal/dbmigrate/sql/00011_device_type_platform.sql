-- Default platform on a device type, copied onto devices at create time.
--
-- +goose Up

ALTER TABLE public.device_types
    ADD COLUMN IF NOT EXISTS platform_id bigint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_device_types_platform_id ON public.device_types (platform_id);

-- +goose Down

DROP INDEX IF EXISTS public.idx_device_types_platform_id;
ALTER TABLE public.device_types DROP COLUMN IF EXISTS platform_id;
