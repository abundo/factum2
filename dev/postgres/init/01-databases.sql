-- Extra databases on the shared Postgres instance.
-- The image already creates POSTGRES_DB (factum2) owned by POSTGRES_USER.
CREATE USER netbox WITH PASSWORD 'netbox';
CREATE DATABASE netbox OWNER netbox;
\connect netbox
GRANT ALL ON SCHEMA public TO netbox;
GRANT CREATE ON SCHEMA public TO netbox;
