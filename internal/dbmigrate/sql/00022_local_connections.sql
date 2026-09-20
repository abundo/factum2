-- Allow many Factum-local cables (netbox_id = 0). NetBox cable ids stay unique.
--
-- +goose Up

DROP INDEX IF EXISTS public.idx_connections_netbox_id;
CREATE UNIQUE INDEX idx_connections_netbox_id ON public.connections (netbox_id) WHERE netbox_id != 0;

-- +goose Down

DROP INDEX IF EXISTS public.idx_connections_netbox_id;
CREATE UNIQUE INDEX idx_connections_netbox_id ON public.connections USING btree (netbox_id);
