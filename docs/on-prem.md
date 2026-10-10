# On-prem profile

Set `DEPLOYMENT_MODE=onprem`. The profile keeps `tenant_id`, forced RLS, authorization, and API behavior identical to SaaS, but idempotently provisions one tenant, project, exact embed origin, and owner membership.

## Required bootstrap values

Set `ONPREM_TENANT_NAME`, `ONPREM_PROJECT_NAME`, `ONPREM_PROJECT_KEY`, `ONPREM_PROJECT_ORIGIN`, `ONPREM_OWNER_SUBJECT`, and `ONPREM_OWNER_EMAIL`. The owner subject must exactly equal the OIDC `sub` claim and the configured public issuer must equal `iss`. Bootstrap is restart-safe. Changing the subject adds a new owner; remove obsolete memberships deliberately.

`LOCAL_DEMO_SEED` must remain `false`. It exists only for the repository's local Compose fixture and provisions known test identities.

## Production requirements

- Terminate TLS at a trusted reverse proxy. Expose PostgreSQL and Keycloak administration only on private networks.
- Use PostgreSQL 17 with separate migration-owner and runtime credentials. Startup rejects a runtime role that is superuser, has `BYPASSRLS`, or owns application tables.
- Configure exact public API, web, OIDC, MCP, and project origins. Wildcards, paths on origins, query strings, protocol-relative URLs, and `null` origins fail startup.
- Use `STORAGE_DRIVER=filesystem` and a private persistent path outside the web root. Startup probes writability; blobs are random-keyed and mode `0600`.
- Back up PostgreSQL and the attachment directory as one recovery point. Database rows without their matching blob snapshot are incomplete.
- Rotate OIDC client secrets, embed signing keys, PATs, database passwords, and proxy credentials. PAT revocation state is persisted in PostgreSQL.
- Run migrations before traffic by starting the API. Migrations and bootstrap are idempotent.

Use `docker compose -f docker-compose.yml -f compose.production.yml up -d` for Internet-facing operation. Set `APP_ENV=production`; this rejects `LOCAL_DEMO_SEED=true` and non-TLS public API/web/OIDC URLs. The production overlay removes direct service ports, runs Keycloak in production mode against PostgreSQL, and exposes only Caddy on ports 80/443.

Health endpoints distinguish process liveness (`/livez`) from PostgreSQL health (`/healthz`) and complete storage/database readiness (`/readyz`). Scrape `/metrics` from a protected monitoring network. Rate limits protect embed authentication, ticket mutations, uploads, and MCP.

Run `BACKUP_DIR=/secure/path ./scripts/backup.sh` from the deployment directory. The script writes a PostgreSQL custom-format dump, attachment-volume archive, and SHA-256 manifest under one timestamped recovery point.

Restore a complete recovery point only during a write outage: `BACKUP_PATH=/secure/path/<timestamp> POSTGRES_MIGRATION_USER=backlog_migrator POSTGRES_DB=backlog_killer ./scripts/restore.sh`. The command verifies `SHA256SUMS`, replaces database objects from the custom-format dump, then replaces attachment-volume contents.

## Upgrade and restore

1. Stop writes or take an application-consistent database and attachment snapshot.
2. Start the new API image against staging copies; it applies pending migrations before binding HTTP.
3. Exercise OIDC login, `/v1/me`, ticket read/write, attachment upload/download, PAT revocation, and RLS isolation.
4. Restore both PostgreSQL and attachments together when rolling back data. Image rollback alone does not reverse migrations.
5. After disaster recovery, verify the runtime role attributes, forced-RLS flags, owner login, project origin, attachment hashes, and revoked PAT behavior before reopening traffic.
