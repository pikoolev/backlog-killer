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

## Upgrade and restore

1. Stop writes or take an application-consistent database and attachment snapshot.
2. Start the new API image against staging copies; it applies pending migrations before binding HTTP.
3. Exercise OIDC login, `/v1/me`, ticket read/write, attachment upload/download, PAT revocation, and RLS isolation.
4. Restore both PostgreSQL and attachments together when rolling back data. Image rollback alone does not reverse migrations.
5. After disaster recovery, verify the runtime role attributes, forced-RLS flags, owner login, project origin, attachment hashes, and revoked PAT behavior before reopening traffic.
