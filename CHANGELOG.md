# Changelog

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
