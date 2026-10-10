# Architecture and trust boundaries

## Components

- `cmd/server`: one Go HTTP binary exposing REST, embed auth, attachment streams, and stateless Streamable HTTP MCP.
- `internal/backlog`: the only ticket transition, claim, bulk, and completion transaction boundary. REST and MCP map into it.
- PostgreSQL: tenant/project composite keys plus forced RLS. Runtime queries execute in transactions after `set_config('app.tenant_id', tenant, true)`.
- Keycloak: OIDC identity. Authorization comes from Backlog Killer invitations and memberships, never email or realm presence alone.
- `apps/web`: dashboard. It treats server permissions as authoritative.
- `packages/sdk`: untrusted-origin client in Shadow DOM. Public project keys locate projects; short-lived bearer tokens authorize requests.
- Filesystem storage: private blobs addressed by random keys with `0600` permissions. Access always passes through API authorization; unsupported storage drivers fail startup.

## Boundaries

Browser input, OpenAPI payloads, OIDC claims, PATs, MCP calls, filenames, and stored anchor JSON are untrusted. Tenant identity is derived from authenticated membership or project-bound PAT, never request bodies. Inaccessible and cross-project objects return 404. Bulk and claim prevalidate all IDs and commit atomically.

Migration credentials own schema objects but cannot log in at runtime. `backlog_runtime` is non-owner, non-superuser, and cannot bypass RLS. Missing tenant context denies business rows.

Logs contain request IDs and operational metadata only: no bearer tokens, invitation codes, query strings, ticket bodies, form values, or attachment bytes.

## Runtime controls

The API exposes separate liveness and readiness probes, Prometheus-format request counters, bounded request bodies/timeouts, and per-principal/IP rate limits. A lifecycle worker retries attachment deletion outbox records and expires old embed flows, idempotency responses, and invitations. Project CORS remains exact-origin; dashboard CORS permits only the configured `WEB_PUBLIC_URL`.

Production runs behind the pinned Caddy TLS proxy. PostgreSQL, Keycloak, API, and dashboard do not publish host ports in the production overlay. Keycloak uses PostgreSQL rather than its development database. `APP_ENV=production` fails closed when demo seed is enabled or a public URL is not HTTPS.
