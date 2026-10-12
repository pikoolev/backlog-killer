# Changelog
## Unreleased — Production hardening

- Bound embed, mutation, upload, and MCP rate-limit state without retaining bearer credentials in memory.
- Added cursor pagination end to end, stale-response guards, project-safe ticket details, and bounded attachment download URLs.
- Restricted annotation to explicit right click and screenshots to the selected element with form/private-content masking.
- Hardened tenant/project authorization, invitation acceptance, claim restore/delete handling, on-prem bootstrap, attachment quotas, deletion reconciliation, and platform-admin issuer binding.
- Added complete OpenAPI administration/embed DTOs, an executable production Compose smoke, managed Keycloak realm updates, and consistent application/Keycloak/attachment backup and restore.
- Embedded the platform client catalog into the Administration sidebar view, normalized origin input, and added in-form progress, success, and error feedback for origins and agent tokens.
- Allowed the exact dashboard URL through project-scoped CORS so administrators can add a project's first client origin; embed origins remain restricted to the project's allowlist.
- Added dedicated super-admin Clients, Projects, and Users sidebar catalogs plus dashboard ticket creation and inline backlog deletion.
- Allowed only the pinned production extension origin through project CORS, added in-page yellow ticket notes, two-button element selection, immediate post-create canvas refresh, and collapsed extension statistics.


## 0.2.0 — Local SaaS release candidate

- Added `*.localhost` client, dashboard, API, and identity endpoints configured through `.env`.
- Rebuilt the dashboard around backlog, in-progress/rework, review, completed, deleted, client administration, users, SDK origins, PATs, and active agent assignments.
- Restyled visual tickets as yellow sticky notes based on the proven concertCRM interaction pattern.
- Added a polished local client HTML surface with embedded SDK behavior.
- Published the SDK bundle from the dashboard image at `/sdk/backlog-killer.js` with the correct MIME type.
- Added platform-administrator allowlisting for manual tenant creation.
- Added authorized MCP attachment retrieval for agent OCR/vision/PDF/text processing.
- Split local and production Keycloak realms; production contains no demo identities and uses a separate PostgreSQL service.
- Made frontend API/OIDC addresses and CSP build/runtime configurable for VPS migration.
- Added local-stack and updated on-prem/agent workflow documentation.
