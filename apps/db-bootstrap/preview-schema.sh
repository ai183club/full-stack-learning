#!/bin/sh
set -eu

: "${PGHOST:?PGHOST is required}"
: "${PGPORT:?PGPORT is required}"
: "${PGDATABASE:?PGDATABASE is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PR_NUMBER:?PR_NUMBER is required}"
: "${PREVIEW_DB_PASSWORD:?PREVIEW_DB_PASSWORD is required}"

case "$PR_NUMBER" in
  *[!0-9]* | '' | 0 | 0*)
    printf '%s\n' 'PR_NUMBER must be a positive decimal integer without leading zeroes' >&2
    exit 64
    ;;
esac

if [ "${#PR_NUMBER}" -gt 9 ]; then
  printf '%s\n' 'PR_NUMBER must contain at most 9 digits' >&2
  exit 64
fi

export PGSSLMODE="${PGSSLMODE:-verify-full}"
export PGSSLROOTCERT="${PGSSLROOTCERT:-/usr/local/share/ca-certificates/rds-global-bundle.pem}"

schema="pr_${PR_NUMBER}"
role="profile_pr_${PR_NUMBER}"

printf '%s\n' "preview schema bootstrap started: schema=${schema}, role=${role}"

if [ "${PREVIEW_CLEANUP:-false}" = "true" ]; then
  psql --no-password --set=ON_ERROR_STOP=1 --set="preview_schema=${schema}" --set="preview_role=${role}" <<'SQL'
SELECT format('DROP SCHEMA IF EXISTS %I CASCADE', :'preview_schema')
\gexec
SELECT format('REVOKE CONNECT ON DATABASE %I FROM %I', current_database(), :'preview_role')
WHERE EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = :'preview_role'
)
\gexec
SELECT format('DROP ROLE %I', :'preview_role')
WHERE EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = :'preview_role'
)
\gexec
SQL
  printf '%s\n' "preview schema cleanup completed: schema=${schema}"
  exit 0
fi

psql --no-password --set=ON_ERROR_STOP=1 \
  --set="preview_schema=${schema}" \
  --set="preview_role=${role}" \
  --set="preview_password=${PREVIEW_DB_PASSWORD}" <<'SQL'
SELECT EXISTS (
    SELECT 1
    FROM pg_namespace AS namespace
    CROSS JOIN LATERAL aclexplode(COALESCE(namespace.nspacl, acldefault('n', namespace.nspowner))) AS privilege
    WHERE namespace.nspname = 'public'
      AND privilege.grantee = 0
      AND privilege.privilege_type IN ('USAGE', 'CREATE')
) AS public_schema_has_public_access
\gset

\if :public_schema_has_public_access
SELECT 1 / 0;
\endif

SELECT format('CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'preview_role', :'preview_password')
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = :'preview_role'
)
\gexec

SELECT format('ALTER ROLE %I LOGIN NOINHERIT NOCREATEDB NOCREATEROLE PASSWORD %L', :'preview_role', :'preview_password')
\gexec

SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'preview_role')
\gexec

SELECT format('CREATE SCHEMA IF NOT EXISTS %I', :'preview_schema')
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON SCHEMA %I FROM PUBLIC', :'preview_schema')
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %I FROM PUBLIC', :'preview_schema')
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %I FROM PUBLIC', :'preview_schema')
\gexec

SELECT format('GRANT USAGE ON SCHEMA %I TO %I', :'preview_schema', :'preview_role')
\gexec

SELECT format(
    'CREATE TABLE IF NOT EXISTS %I.profiles (
        id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
        username VARCHAR(32) NOT NULL UNIQUE,
        password_hash VARCHAR(255) NOT NULL,
        name VARCHAR(80) NOT NULL,
        bio TEXT NOT NULL DEFAULT '''',
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )',
    :'preview_schema'
)
\gexec

SELECT format(
    'CREATE TABLE IF NOT EXISTS %I.bio_generation_jobs (
        job_id VARCHAR(36) PRIMARY KEY,
        username VARCHAR(32) NOT NULL UNIQUE,
        name VARCHAR(80) NOT NULL,
        status VARCHAR(16) NOT NULL DEFAULT ''pending''
            CHECK (status IN (''pending'', ''running'', ''completed'', ''failed'')),
        error_code VARCHAR(64),
        attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
        lease_expires_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )',
    :'preview_schema'
)
\gexec

SELECT format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.profiles TO %I', :'preview_schema', :'preview_role')
\gexec

SELECT format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.bio_generation_jobs TO %I', :'preview_schema', :'preview_role')
\gexec

SELECT format('GRANT USAGE, SELECT ON SEQUENCE %I.profiles_id_seq TO %I', :'preview_schema', :'preview_role')
\gexec

SELECT format('ALTER ROLE %I SET search_path TO %I', :'preview_role', :'preview_schema')
\gexec
SQL

printf '%s\n' "preview schema bootstrap completed: schema=${schema}"
