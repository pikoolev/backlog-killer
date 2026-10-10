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
