-- Icinga Web + Icinga DB on the shared MariaDB (librenms is MYSQL_DATABASE).
CREATE DATABASE IF NOT EXISTS icingadb;
CREATE DATABASE IF NOT EXISTS icingaweb;
CREATE USER IF NOT EXISTS 'icingadb'@'%' IDENTIFIED BY 'icingadb';
CREATE USER IF NOT EXISTS 'icingaweb'@'%' IDENTIFIED BY 'icingaweb';
GRANT ALL PRIVILEGES ON icingadb.* TO 'icingadb'@'%';
GRANT ALL PRIVILEGES ON icingaweb.* TO 'icingaweb'@'%';
FLUSH PRIVILEGES;
