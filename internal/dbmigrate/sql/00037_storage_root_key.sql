-- storage_root stores a worker.paths key. The directory stays in each
-- storage host's factum2-worker.yaml (worker.paths.storage_root). A
-- previous value was the directory itself; rewrite it to the key. Copy
-- a custom directory into the worker YAML before migrating, because this
-- statement does not keep the old path.
--
-- +goose Up

UPDATE public.settings SET storage_root = 'storage_root'
    WHERE storage_root IS NOT NULL AND btrim(storage_root) <> '';

-- +goose Down

-- The old filesystem path is not stored. Leave the key in place.
SELECT 1;
