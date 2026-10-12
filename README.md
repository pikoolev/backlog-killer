# Backlog Killer

Multi-tenant visual backlog for human-to-agent software delivery. A React dashboard and embeddable Shadow DOM SDK share a Go/PostgreSQL workflow API and MCP server.

## Local runtime

Requirements: Docker Desktop, Go 1.25+, Node 22+, pnpm 10.2.

```sh
cp .env.example .env
# Replace local passwords and signing key, then:
docker compose up --build -d
docker compose ps
```

Local domains:

- Client HTML + SDK: `http://client.localhost:4174`
- Dashboard: `http://backlog.localhost:4173`
- API/MCP: `http://api.localhost:18080`, `/mcp`
- Keycloak: `http://identity.localhost:8081`

Seed users are `owner`, `triager`, and `reporter` at `example.test`; local-only passwords are in `infra/keycloak/realm.local.json`. `owner` is also the local platform administrator who can manually create client tenants. See `docs/local-stack.md` for the end-to-end local workflow.

## SDK

The dashboard image publishes the SDK bundle at `/sdk/backlog-killer.js`:

```html
<script async src="https://backlog.example.com/sdk/backlog-killer.js"
  data-project-key="public-project-key"
  data-api-base="https://api.example.com"></script>
```

Add the client's exact origin in Administration before loading the SDK. `projectKey` is public. Browser authorization uses short-lived user tokens; never embed a PAT or project secret.
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

1. A reporter signs into the embedded SDK, selects a DOM element with an explicit right click, and creates a page-scoped ticket. Operational tickets remain as persistent pins and can be focused with `?backlog-ticket=<uuid>`.
2. A triager edits the title/specification/priority/due date and atomically moves the ticket to `in_progress`.
3. An agent uses a project PAT with REST or `/mcp`, reads the complete event/attachment history, claims an idempotent batch, and submits `agentComment` plus durable `workLog`.
4. An administrator accepts review work or returns it to rework with a mandatory comment. Deleted tickets restore only to their recorded previous status.

The dashboard includes project switching, origins, members/invitations, PAT creation/revocation, ticket history, attachments, bulk updates, review, completion, deletion, and restore.

## Production deployment

The repository supports two production profiles:

- Native Ubuntu 24.04 deployment: populate `.env`, then run `powershell -ExecutionPolicy Bypass -File scripts/deploy-native.ps1`. The launcher reads `VPS_PASSWORD` locally and authenticates through Paramiko without placing the password in process arguments or logs. See `docs/native-deployment.md`.
- Container deployment: use `docker-compose.yml` with `compose.production.yml` and the explicit secrets required by that overlay.

The native profile serves the future landing host, SDK, panel, API/MCP, and OIDC on separate TLS hosts. Caddy obtains and renews certificates. PostgreSQL, Keycloak, and the API bind only to the VPS loopback or local PostgreSQL socket. Root password SSH access remains enabled as the required break-glass path.

Runtime probes: `/livez`, `/healthz`, `/readyz`; Prometheus text metrics: `/metrics`.

## Release gate

CI starts PostgreSQL, Keycloak, API, dashboard, and demo; applies fresh migrations; runs race/integration/unit tests, generated-contract checks, builds, Playwright workflows, dependency vulnerability gates, and validates the production Compose profile.

## License

Backlog Killer is available under the MIT License. Third-party attribution and distribution guidance are in `THIRD_PARTY_NOTICES.md`.
