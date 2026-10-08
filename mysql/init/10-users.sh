#!/bin/bash
# The database's users and exactly what each may do. See docs/mysql-migration.md,
# D26.
#
# Runs by itself the first time the data volume is created (mounted as
# /docker-entrypoint-initdb.d). On a volume that already exists MySQL skips it,
# so run it by hand, as often as needed:
#
#   docker compose exec mysql /docker-entrypoint-initdb.d/10-users.sh
#
# It is the one place users and permissions are defined. Each run first takes
# everything away and then grants what is written here, so a permission added by
# hand somewhere else does not survive it, and a user no longer listed here is
# removed. A new permission goes into this file.
#
#   app  the viewer and its Flyway migrations. One user for both (D26), so it
#        may change the schema: principle 1 and ddl-auto=validate are kept by
#        configuration, not enforced by MySQL.
#
# '%' for the host because connections arrive through Docker's port forwarding,
# from its gateway address rather than from localhost. Safe only because the
# port is bound to 127.0.0.1 (D22).

set -euo pipefail

create_users() {
    local database="${MYSQL_DATABASE:?}"
    local app_password="${APP_DB_PASSWORD:?set APP_DB_PASSWORD in mysql/.env}"

    # Passwords go in on stdin, not on the command line, so they never show up
    # in the process list.
    MYSQL_PWD="${MYSQL_ROOT_PASSWORD:?}" mysql --protocol=socket -uroot <<SQL
-- Replaced by 'app' (D26, superseding D25).
DROP USER IF EXISTS 'flyway'@'%', 'viewer'@'%';

CREATE USER IF NOT EXISTS 'app'@'%' IDENTIFIED BY '${app_password}';
ALTER USER 'app'@'%' IDENTIFIED BY '${app_password}';
REVOKE ALL PRIVILEGES, GRANT OPTION FROM 'app'@'%';
GRANT CREATE, ALTER, DROP, INDEX, REFERENCES, SELECT, INSERT, UPDATE, DELETE
    ON \`${database}\`.* TO 'app'@'%';
SQL

    echo "10-users.sh: app set up on ${database}"
}

create_users
