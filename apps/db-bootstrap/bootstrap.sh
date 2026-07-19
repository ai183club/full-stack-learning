#!/bin/sh
set -eu

: "${PGHOST:?PGHOST is required}"
: "${PGPORT:?PGPORT is required}"
: "${PGDATABASE:?PGDATABASE is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PROFILE_APP_USER:?PROFILE_APP_USER is required}"
: "${PROFILE_APP_PASSWORD:?PROFILE_APP_PASSWORD is required}"

export PGSSLMODE="${PGSSLMODE:-verify-full}"
export PGSSLROOTCERT="${PGSSLROOTCERT:-/usr/local/share/ca-certificates/rds-global-bundle.pem}"

printf '%s\n' "database bootstrap started: database=${PGDATABASE}, application_user=${PROFILE_APP_USER}"

psql --no-password --set=ON_ERROR_STOP=1 \
  --set="app_user=${PROFILE_APP_USER}" \
  --set="app_password=${PROFILE_APP_PASSWORD}" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', :'app_user')
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = :'app_user'
)
\gexec

SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', :'app_user', :'app_password')
\gexec

SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'app_user')
\gexec

REVOKE ALL PRIVILEGES ON SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC;

SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'app_user')
\gexec

CREATE TABLE IF NOT EXISTS public.profiles (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username VARCHAR(32) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(80) NOT NULL,
    bio TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

SELECT format(
    'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.profiles TO %I',
    :'app_user'
)
\gexec

SELECT format(
    'GRANT USAGE, SELECT ON SEQUENCE public.profiles_id_seq TO %I',
    :'app_user'
)
\gexec
SQL

printf '%s\n' "database bootstrap completed"
