# Changelog
## Unreleased — Production hardening

- Bound embed, mutation, upload, and MCP rate-limit state without retaining bearer credentials in memory.
- Added cursor pagination end to end, stale-response guards, project-safe ticket details, and bounded attachment download URLs.
- Restricted annotation to explicit right click and screenshots to the selected element with form/private-content masking.
- Hardened tenant/project authorization, invitation acceptance, claim restore/delete handling, on-prem bootstrap, attachment quotas, deletion reconciliation, and platform-admin issuer binding.
- Added complete OpenAPI administration/embed DTOs, an executable production Compose smoke, managed Keycloak realm updates, and consistent application/Keycloak/attachment backup and restore.


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
