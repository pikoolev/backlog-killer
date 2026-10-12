# Native Ubuntu deployment

The native profile targets a clean Ubuntu 24.04 amd64 VPS and does not install Docker. It installs PostgreSQL 17, Keycloak 26.2.5, Caddy, UFW, fail2ban, Go 1.25.4, and Node 22.15.1.

## Public hosts

- `APP_DOMAIN`: reserved landing page; returns 404 until landing content is published.
- `SDK_PUBLIC_HOST`: public `backlog-killer.js`.
- `PANEL_PUBLIC_HOST`: dashboard SPA.
- `API_PUBLIC_HOST`: REST, MCP, and health endpoints.
- `OIDC_PUBLIC_HOST`: Keycloak/OIDC.

All A records must resolve to `VPS_IP`. Do not publish AAAA records unless the VPS has working IPv6. Provider firewall rules must allow the configured SSH port and TCP 80/443.

## Run

Populate `.env`; it is ignored by Git and excluded from the uploaded archive. Then run from Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/deploy-native.ps1
```

The launcher reads `VPS_PASSWORD` from the Git-ignored local `.env` and passes it directly to Paramiko. It never places the password in process arguments, the source archive, remote configuration, or logs.

The provisioner creates database, Keycloak, and signing secrets on the VPS in `/etc/backlog-killer/secrets.env` with mode `0600`. The uploaded deployment configuration is deleted on success or failure.

## SSH and fail2ban invariant

The provisioner writes an SSH drop-in preserving:

```text
PermitRootLogin yes
PasswordAuthentication yes
KbdInteractiveAuthentication yes
```

It validates the complete SSH configuration before reloading SSH. UFW always allows `VPS_SSH_PORT` before it is enabled.

The fail2ban jail blocks repeated attempts for nonexistent SSH users but deliberately excludes failed authentication for the valid `root` break-glass account. The source IP of the active deployment session and optional `VPS_TRUSTED_IP_CIDR` are also added to `ignoreip`. This keeps fail2ban active without allowing it to lock the only emergency account.

## Post-deployment checks

The provisioner verifies local API readiness before completing. Then verify externally:

```text
https://API_PUBLIC_HOST/readyz
https://OIDC_PUBLIC_HOST/realms/backlog-killer/.well-known/openid-configuration
https://PANEL_PUBLIC_HOST/
https://SDK_PUBLIC_HOST/backlog-killer.js
```

Keep the original SSH session open and prove a second root password login before treating the firewall setup as complete.
