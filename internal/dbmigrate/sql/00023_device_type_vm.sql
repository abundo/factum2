-- Device types can be virtual machines. NetBox dcim device types stay false;
-- a virtual machine that carries a model sets the flag on sync.
--
-- +goose Up

ALTER TABLE public.device_types
    ADD COLUMN IF NOT EXISTS vm boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE public.device_types DROP COLUMN IF EXISTS vm;
