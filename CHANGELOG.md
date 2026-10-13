# Changelog

## 0.2.4 — Screen-aware sticker canvas

- Replaced the two-button anchor gesture with an isolated page-wide canvas: left click places a preview sticker, Escape cancels, and the panel saves its selected screen and click position with the ticket.
- Dynamically injects and verifies the content script for already-open pages; reinjection tears down previous listeners without leaving stale canvases.
- Restores sticky notes only on their exact page URL/internal tab and keeps anchored positions aligned across scrolling and resizing.
- Verified live Chromium login, page-script injection, ordinary left-click selection, Escape and ticket creation through the production API.

## 0.2.3 — ConcertCRM extension release

- Rebuilt the published extension to replace the stale 0.2.2 panel; unauthenticated users now see login rather than a non-working logout button.
- Isolated V4 sessions, required explicit OIDC reauthentication, and prevented late refresh/login/API results from restoring signed-out state. Logout clears page notes and revokes the refresh token without waiting for the identity provider.
- Resolved the active tab in the side panel's own window and discarded stale project, tab, and ticket responses.
- Verified production PKCE login, exact ConcertCRM origin matching, real page-element selection, ticket creation and rendering, no-site logout, and a delayed-refresh/logout race in Chromium.
- Preserved existing Keycloak passwords during reprovisioning and configured the extension audience from the deployment setting.

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
- Versioned extension sessions, forced explicit login after install/update, made logout clear session and every canvas atomically, and moved active web-tab discovery to the background worker.
- Isolated extension auth in a v3 storage key, rejected late OIDC callbacks after logout, bound the side panel to the toolbar-selected web tab, and made logout update the UI before background canvas cleanup.
- Kept `sidePanel.open()` inside the synchronous toolbar click gesture while persisting the bound tab independently, avoiding Chromium's user-gesture rejection.


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
