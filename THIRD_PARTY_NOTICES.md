# Third-party notices

Backlog Killer includes third-party software under permissive licenses. Copyright remains with each upstream author. Full dependency versions are pinned in `go.sum` and `pnpm-lock.yaml`; source distributions and license texts are available from the linked upstream projects.

## Runtime libraries

- React, React DOM, TanStack Query/Table, React Router, html2canvas, Zod, and their transitive browser libraries — MIT.
- Keycloak JavaScript adapter — Apache License 2.0.
- chi, golang-jwt, google/uuid, pgx, MCP Go SDK, and `golang.org/x/*` Go modules — BSD/MIT/Apache-2.0-style permissive licenses as declared by their modules.

## Runtime images

- PostgreSQL Alpine image — PostgreSQL License plus Alpine package licenses.
- Keycloak image — Apache License 2.0 plus bundled dependency licenses.
- Alpine, Go, Node, and nginx build/runtime images — their respective upstream and bundled package licenses.

The anchor implementation in `packages/sdk/src/anchor` is original for this repository. No SitePing source code was copied or adapted.

Before redistribution, regenerate the inventory with `pnpm licenses list --prod --json` and `go list -m -json all`, retain upstream license files in the distributed artifact, and review container SBOMs for the exact image digests.
