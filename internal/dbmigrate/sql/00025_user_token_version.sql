-- Session revocation. The login JWT carries token_version and
-- RequireAPIAuth rejects a cookie whose version does not match the row.
-- Password changes increment the column.
--
-- +goose Up

ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS token_version integer NOT NULL DEFAULT 1;

-- +goose Down

ALTER TABLE public.users DROP COLUMN IF EXISTS token_version;
