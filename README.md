# Backlog Killer

Multi-tenant visual backlog for human-to-agent software delivery. A React dashboard and embeddable Shadow DOM SDK share a Go/PostgreSQL workflow API and MCP server.

## Local runtime

Requirements: Docker Desktop, Go 1.25+, Node 22+, pnpm 10.2.

```sh
cp .env.example .env
# Replace every change-me value and EMBED_SIGNING_KEY. Local Compose explicitly
# enables LOCAL_DEMO_SEED; never enable it in a deployed environment.
docker compose up --build -d
docker compose ps
```

URLs:

- Dashboard: `http://127.0.0.1:4173`
- SDK demo: `http://127.0.0.1:4174`
- API/MCP: `http://127.0.0.1:18080`, `/mcp`
- Keycloak: `http://127.0.0.1:8081`

Seed users are `owner`, `triager`, and `reporter` at `example.test`; local-only passwords are in `infra/keycloak/realm.json`. Change or remove them outside local development.

## SDK

```html
<script async src="https://dashboard.example/backlog-killer.js"
  data-project-key="public-project-key"
  data-api-base="https://api.example"></script>
```

`projectKey` is public. Browser authorization uses short-lived user tokens; never embed a PAT or project secret.

## Commands

```sh
pnpm install --frozen-lockfile
pnpm generate
pnpm typecheck && pnpm test && pnpm build
go test -race ./...
go vet ./...
docker compose config
docker compose down
```

OpenAPI is canonical at `openapi/openapi.yaml`; `pnpm check:generated` fails when `packages/contracts/src/generated.ts` is stale.

## Profiles

`DEPLOYMENT_MODE=saas` enables multi-tenant operation. `DEPLOYMENT_MODE=onprem` idempotently bootstraps one configured tenant, owner, project, and exact embed origin while retaining forced RLS and the same APIs. The dashboard discovers accessible projects through `/v1/me`; it has no fixed tenant/project IDs. See `docs/on-prem.md` for required settings, backup, restore, and upgrade checks.

## Working workflow

1. A reporter signs into the embedded SDK, selects a DOM element, and creates a page-scoped ticket. Operational tickets remain as persistent pins and can be focused with `#backlog-ticket=<uuid>`.
2. A triager edits the title/specification/priority/due date and atomically moves the ticket to `in_progress`.
3. An agent uses a project PAT with REST or `/mcp`, reads the complete event/attachment history, claims an idempotent batch, and submits `agentComment` plus durable `workLog`.
4. An administrator accepts review work or returns it to rework with a mandatory comment. Deleted tickets restore only to their recorded previous status.

The dashboard includes project switching, origins, members/invitations, PAT creation/revocation, ticket history, attachments, bulk updates, review, completion, deletion, and restore.

## Production deployment

Local Compose is a development fixture. Production must use both files and explicit secrets/hosts:

```sh
export API_PUBLIC_HOST=api.example.com WEB_PUBLIC_HOST=backlog.example.com OIDC_PUBLIC_HOST=identity.example.com
export DEPLOYMENT_MODE=saas POSTGRES_MIGRATION_USER=backlog_migrator POSTGRES_MIGRATION_PASSWORD='...'
export POSTGRES_PASSWORD='...' KEYCLOAK_ADMIN_USERNAME=admin KEYCLOAK_ADMIN_PASSWORD='...' EMBED_SIGNING_KEY='...'
docker compose -f docker-compose.yml -f compose.production.yml config
docker compose -f docker-compose.yml -f compose.production.yml up -d
```

The production overlay disables the demo site and seed, removes direct database/identity/API/web ports, gives Keycloak persistent PostgreSQL storage, and terminates TLS through Caddy. Startup rejects demo seed and HTTP public URLs when `APP_ENV=production`.

Runtime probes: `/livez`, `/healthz`, `/readyz`; Prometheus text metrics: `/metrics`. Back up PostgreSQL and attachments as one recovery point with `BACKUP_DIR=/secure/path ./scripts/backup.sh`.

## Release gate

CI starts PostgreSQL, Keycloak, API, dashboard, and demo; applies fresh migrations; runs race/integration/unit tests, generated-contract checks, builds, Playwright workflows, dependency vulnerability gates, and validates the production Compose profile.

## License

Backlog Killer is available under the MIT License. Third-party attribution and distribution guidance are in `THIRD_PARTY_NOTICES.md`.
