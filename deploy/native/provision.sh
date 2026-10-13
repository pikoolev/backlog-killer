#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive

CONFIG_FILE=${1:-/root/.backlog-killer-deploy.env}
[[ $EUID -eq 0 ]] || { echo 'provision.sh must run as root' >&2; exit 1; }
[[ -f "$CONFIG_FILE" ]] || { echo 'deployment config not found' >&2; exit 1; }
# shellcheck disable=SC1090
source "$CONFIG_FILE"
cleanup_config() { rm -f "$CONFIG_FILE"; }
trap cleanup_config EXIT


required=(APP_DOMAIN SDK_PUBLIC_HOST PANEL_PUBLIC_HOST API_PUBLIC_HOST OIDC_PUBLIC_HOST ACME_EMAIL INITIAL_ADMIN_EMAIL INITIAL_ADMIN_USERNAME INITIAL_ADMIN_PASSWORD POSTGRES_DB POSTGRES_MIGRATION_USER OIDC_REALM OIDC_AUDIENCE OIDC_CLIENT_ID DEPLOYMENT_MODE STORAGE_PROJECT_QUOTA_BYTES)
for name in "${required[@]}"; do
  [[ -n ${!name:-} ]] || { echo "missing required setting: $name" >&2; exit 1; }
done
[[ $(. /etc/os-release && printf '%s' "$ID:$VERSION_ID") == ubuntu:24.04 ]] || { echo 'Ubuntu 24.04 is required' >&2; exit 1; }
[[ $(dpkg --print-architecture) == amd64 ]] || { echo 'amd64 VPS is required' >&2; exit 1; }
for host in "$APP_DOMAIN" "$SDK_PUBLIC_HOST" "$PANEL_PUBLIC_HOST" "$API_PUBLIC_HOST" "$OIDC_PUBLIC_HOST"; do
  [[ $host =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || { echo "invalid hostname: $host" >&2; exit 1; }
done

log() { printf '[deploy] %s\n' "$1"; }
random_secret() { openssl rand -hex 32 | tr -d '\n'; }
install -d -m 0700 /etc/backlog-killer
SECRETS=/etc/backlog-killer/secrets.env
if [[ ! -f $SECRETS ]]; then
  cat >"$SECRETS" <<EOF
POSTGRES_PASSWORD=$(random_secret)
POSTGRES_MIGRATION_PASSWORD=$(random_secret)
KEYCLOAK_DB_PASSWORD=$(random_secret)
KEYCLOAK_ADMIN_PASSWORD=$(random_secret)
EMBED_SIGNING_KEY=$(random_secret)
EOF
  chmod 0600 "$SECRETS"
fi
# shellcheck disable=SC1090
source "$SECRETS"

log 'Installing operating-system packages'
rm -f /etc/apt/sources.list.d/pgdg.list /etc/apt/sources.list.d/caddy-stable.list
apt-get update -qq
apt-get dist-upgrade -y -qq
apt-get install -y -qq ca-certificates curl git gnupg jq openssl rsync tar unzip ufw fail2ban openjdk-21-jre-headless debian-keyring debian-archive-keyring apt-transport-https
install -d -m 0755 /etc/apt/keyrings /usr/share/keyrings
curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor --yes -o /etc/apt/keyrings/postgresql.gpg
chmod 0644 /etc/apt/keyrings/postgresql.gpg
printf 'deb [signed-by=/etc/apt/keyrings/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt noble-pgdg main\n' >/etc/apt/sources.list.d/pgdg.list
curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
chmod 0644 /usr/share/keyrings/caddy-stable-archive-keyring.gpg
printf 'deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main\n' >/etc/apt/sources.list.d/caddy-stable.list
apt-get update -qq
apt-get install -y -qq postgresql-17 postgresql-client-17 caddy

log 'Preserving root password SSH access'
install -d -m 0755 /etc/ssh/sshd_config.d
cat >/etc/ssh/sshd_config.d/99-backlog-breakglass.conf <<'EOF'
PermitRootLogin yes
PasswordAuthentication yes
KbdInteractiveAuthentication yes
UsePAM yes
EOF
sshd -t
systemctl reload ssh

log 'Configuring firewall and fail2ban'
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw allow "${VPS_SSH_PORT:-22}/tcp" >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw --force enable >/dev/null
admin_ip=${SSH_CONNECTION%% *}
ignore_ips="127.0.0.1/8 ::1"
[[ -n $admin_ip ]] && ignore_ips+=" $admin_ip"
[[ -n ${VPS_TRUSTED_IP_CIDR:-} ]] && ignore_ips+=" $VPS_TRUSTED_IP_CIDR"
cat >/etc/fail2ban/filter.d/sshd-invalid-user.conf <<'EOF'
[Definition]
failregex = ^<F-MLFID>\s*</F-MLFID><F-CONTENT>Failed \S+ for invalid user .* from <HOST> port \d+(?: ssh\d*)?</F-CONTENT>$
ignoreregex =
journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd
EOF
cat >/etc/fail2ban/jail.d/backlog-sshd.local <<EOF
# Protect SSH from invalid-user scans without ever banning failed logins for
# the valid root break-glass account.
[sshd-invalid-user]
enabled = true
filter = sshd-invalid-user
backend = systemd
port = ${VPS_SSH_PORT:-22}
bantime = 15m
findtime = 10m
maxretry = 6
ignoreip = $ignore_ips
EOF
systemctl enable --now fail2ban
systemctl restart fail2ban

log 'Configuring PostgreSQL roles and databases'
systemctl enable --now postgresql
sudo -u postgres psql -v ON_ERROR_STOP=1 \
  --set=migration_user="$POSTGRES_MIGRATION_USER" \
  --set=migration_password="$POSTGRES_MIGRATION_PASSWORD" \
  --set=runtime_password="$POSTGRES_PASSWORD" \
  --set=app_db="$POSTGRES_DB" \
  --set=keycloak_password="$KEYCLOAK_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD %L', :'migration_user', :'migration_password') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'migration_user') \gexec
SELECT format('ALTER ROLE %I CREATEROLE PASSWORD %L', :'migration_user', :'migration_password') \gexec
SELECT format('CREATE ROLE backlog_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD %L', :'runtime_password') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'backlog_runtime') \gexec
SELECT format('ALTER ROLE backlog_runtime PASSWORD %L', :'runtime_password') \gexec
SELECT 'CREATE ROLE backlog_admin NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT BYPASSRLS' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'backlog_admin') \gexec
SELECT format('GRANT backlog_admin TO %I WITH SET TRUE', :'migration_user') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'app_db', :'migration_user') WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'app_db') \gexec
SELECT format('CREATE ROLE keycloak LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'keycloak_password') WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'keycloak') \gexec
SELECT format('ALTER ROLE keycloak PASSWORD %L', :'keycloak_password') \gexec
SELECT 'CREATE DATABASE keycloak OWNER keycloak' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'keycloak') \gexec
SQL

log 'Installing pinned runtimes'
if [[ ! -x /usr/local/go/bin/go || $(/usr/local/go/bin/go version) != *'go1.25.4 '* ]]; then
  curl -fsSL https://go.dev/dl/go1.25.4.linux-amd64.tar.gz -o /tmp/go.tgz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tgz
  rm -f /tmp/go.tgz
fi
if [[ ! -x /opt/node-v22.15.1-linux-x64/bin/node ]]; then
  curl -fsSL https://nodejs.org/dist/v22.15.1/node-v22.15.1-linux-x64.tar.xz -o /tmp/node.tar.xz
  tar -C /opt -xJf /tmp/node.tar.xz
  rm -f /tmp/node.tar.xz
fi
ln -sfn /opt/node-v22.15.1-linux-x64/bin/node /usr/local/bin/node
ln -sfn /opt/node-v22.15.1-linux-x64/bin/corepack /usr/local/bin/corepack
corepack enable
corepack prepare pnpm@10.2.0 --activate >/dev/null

id backlog >/dev/null 2>&1 || useradd --system --home /var/lib/backlog-killer --shell /usr/sbin/nologin backlog
id keycloak >/dev/null 2>&1 || useradd --system --home /var/lib/keycloak --shell /usr/sbin/nologin keycloak
install -d -o backlog -g backlog -m 0750 /var/lib/backlog-killer/uploads
install -d -o keycloak -g keycloak -m 0750 /var/lib/keycloak
install -d -o keycloak -g keycloak -m 0750 /opt/keycloak/keycloak-26.2.5/data

if [[ ! -x /opt/keycloak/keycloak-26.2.5/bin/kc.sh ]]; then
  curl -fsSL https://github.com/keycloak/keycloak/releases/download/26.2.5/keycloak-26.2.5.tar.gz -o /tmp/keycloak.tar.gz
  install -d /opt/keycloak
  tar -C /opt/keycloak -xzf /tmp/keycloak.tar.gz
  rm -f /tmp/keycloak.tar.gz
  chown -R keycloak:keycloak /opt/keycloak/keycloak-26.2.5
fi
chown -R keycloak:keycloak /opt/keycloak/keycloak-26.2.5

log 'Building and activating application release'
SOURCE=/root/backlog-killer-source
[[ -f $SOURCE/go.mod && -f $SOURCE/pnpm-lock.yaml ]] || { echo 'uploaded source tree is incomplete' >&2; exit 1; }
RELEASE="/opt/backlog-killer/releases/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 0755 "$RELEASE"
(
  cd "$SOURCE"
  /usr/local/go/bin/go build -trimpath -ldflags='-s -w' -o "$RELEASE/server" ./cmd/server
  VITE_API_URL="https://$API_PUBLIC_HOST" VITE_OIDC_URL="https://$OIDC_PUBLIC_HOST" VITE_SDK_URL="https://$SDK_PUBLIC_HOST/backlog-killer.js" VITE_OIDC_REALM="$OIDC_REALM" VITE_OIDC_CLIENT_ID="$OIDC_CLIENT_ID" pnpm install --frozen-lockfile
  VITE_API_URL="https://$API_PUBLIC_HOST" VITE_OIDC_URL="https://$OIDC_PUBLIC_HOST" VITE_SDK_URL="https://$SDK_PUBLIC_HOST/backlog-killer.js" VITE_OIDC_REALM="$OIDC_REALM" VITE_OIDC_CLIENT_ID="$OIDC_CLIENT_ID" pnpm --filter @backlog-killer/web build
  pnpm --filter @backlog-killer/sdk build
  pnpm --filter @backlog-killer/extension build
)
cp -a "$SOURCE/migrations" "$RELEASE/migrations"
cp -a "$SOURCE/apps/web/dist" "$RELEASE/web"
install -d -m 0755 "$RELEASE/sdk"
cp "$SOURCE/packages/sdk/dist/backlog-killer.js" "$RELEASE/sdk/backlog-killer.js"
chown -R root:root "$RELEASE"
cp -a "$SOURCE/apps/extension/dist" "$RELEASE/extension"
(cd "$RELEASE/extension" && zip -qr "$RELEASE/backlog-killer-extension.zip" .)
cp "$RELEASE/backlog-killer-extension.zip" "$RELEASE/sdk/backlog-killer-extension.zip"
chmod -R a+rX "$RELEASE"
chmod 0755 "$RELEASE/server"
ln -sfn "$RELEASE" /opt/backlog-killer/current

cat >/etc/backlog-killer/keycloak.env <<EOF
KC_DB=postgres
KC_DB_URL=jdbc:postgresql://127.0.0.1:5432/keycloak
KC_DB_USERNAME=keycloak
KC_DB_PASSWORD=$KEYCLOAK_DB_PASSWORD
KC_HOSTNAME=$OIDC_PUBLIC_HOST
KC_BOOTSTRAP_ADMIN_USERNAME=admin
KC_BOOTSTRAP_ADMIN_PASSWORD=$KEYCLOAK_ADMIN_PASSWORD
EOF
chmod 0600 /etc/backlog-killer/keycloak.env
cat >/etc/systemd/system/backlog-keycloak.service <<'EOF'
[Unit]
Description=Backlog Killer Keycloak
After=network-online.target postgresql.service
Wants=network-online.target
Requires=postgresql.service

[Service]
Type=simple
User=keycloak
Group=keycloak
EnvironmentFile=/etc/backlog-killer/keycloak.env
ExecStart=/opt/keycloak/keycloak-26.2.5/bin/kc.sh start --health-enabled=true --http-enabled=true --http-host=127.0.0.1 --http-port=8081 --proxy-headers=xforwarded
Restart=on-failure
RestartSec=5
TimeoutStartSec=180
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/keycloak /opt/keycloak/keycloak-26.2.5

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now backlog-keycloak
for _ in {1..90}; do curl -fsS http://127.0.0.1:8081/realms/master >/dev/null 2>&1 && break; sleep 2; done
curl -fsS http://127.0.0.1:8081/realms/master >/dev/null

KCADM=/opt/keycloak/keycloak-26.2.5/bin/kcadm.sh
KCADM_CONFIG=/root/.backlog-killer-kcadm.config
"$KCADM" config credentials --config "$KCADM_CONFIG" --server http://127.0.0.1:8081 --realm master --user admin --password "$KEYCLOAK_ADMIN_PASSWORD" >/dev/null
if ! "$KCADM" get realms/$OIDC_REALM --config "$KCADM_CONFIG" >/dev/null 2>&1; then
  "$KCADM" create realms --config "$KCADM_CONFIG" -s realm="$OIDC_REALM" -s enabled=true -s sslRequired=external -s registrationAllowed=false -s registrationEmailAsUsername=true -s loginWithEmailAllowed=true -s verifyEmail=false -s bruteForceProtected=true -s 'passwordPolicy=length(12) and upperCase(1) and lowerCase(1) and digits(1)' >/dev/null
fi
client_id=$("$KCADM" get clients -r "$OIDC_REALM" --config "$KCADM_CONFIG" -q clientId="$OIDC_CLIENT_ID" --fields id --format csv --noquotes | tr -d '\r')
client_json=$(jq -n --arg id "$OIDC_CLIENT_ID" --arg panel "https://$PANEL_PUBLIC_HOST/*" --arg callback "https://$API_PUBLIC_HOST/embed/auth/callback*" --arg origin "https://$PANEL_PUBLIC_HOST" --arg aud "$OIDC_AUDIENCE" '{clientId:$id,publicClient:true,standardFlowEnabled:true,directAccessGrantsEnabled:false,redirectUris:[$panel,$callback],webOrigins:[$origin],protocolMappers:[{name:"audience",protocol:"openid-connect",protocolMapper:"oidc-audience-mapper",consentRequired:false,config:{"included.custom.audience":$aud,"id.token.claim":"false","access.token.claim":"true"}}]}')
if [[ -z $client_id ]]; then
  printf '%s' "$client_json" | "$KCADM" create clients -r "$OIDC_REALM" --config "$KCADM_CONFIG" -f - >/dev/null
else
  printf '%s' "$client_json" | "$KCADM" update "clients/$client_id" -r "$OIDC_REALM" --config "$KCADM_CONFIG" -f - >/dev/null
fi
extension_client=$("$KCADM" get clients -r "$OIDC_REALM" --config "$KCADM_CONFIG" -q clientId=backlog-killer-extension --fields id --format csv --noquotes | tr -d '\r')
extension_json=$(jq -n --arg aud "$OIDC_AUDIENCE" '{clientId:"backlog-killer-extension",publicClient:true,standardFlowEnabled:true,directAccessGrantsEnabled:false,redirectUris:["https://nkkilejiaedcndfekaibdagbnodglnoe.chromiumapp.org/oidc"],webOrigins:[],protocolMappers:[{name:"audience",protocol:"openid-connect",protocolMapper:"oidc-audience-mapper",consentRequired:false,config:{"included.custom.audience":$aud,"id.token.claim":"false","access.token.claim":"true"}}]}')
if [[ -z $extension_client ]]; then
  printf '%s' "$extension_json" | "$KCADM" create clients -r "$OIDC_REALM" --config "$KCADM_CONFIG" -f - >/dev/null
else
  printf '%s' "$extension_json" | "$KCADM" update "clients/$extension_client" -r "$OIDC_REALM" --config "$KCADM_CONFIG" -f - >/dev/null
fi
user_id=$("$KCADM" get users -r "$OIDC_REALM" --config "$KCADM_CONFIG" -q username="$INITIAL_ADMIN_USERNAME" --fields id --format csv --noquotes | tr -d '\r')
if [[ -z $user_id ]]; then
  "$KCADM" create users -r "$OIDC_REALM" --config "$KCADM_CONFIG" -s username="$INITIAL_ADMIN_USERNAME" -s email="$INITIAL_ADMIN_EMAIL" -s enabled=true -s emailVerified=true >/dev/null
  user_id=$("$KCADM" get users -r "$OIDC_REALM" --config "$KCADM_CONFIG" -q username="$INITIAL_ADMIN_USERNAME" --fields id --format csv --noquotes | tr -d '\r')
  "$KCADM" set-password -r "$OIDC_REALM" --config "$KCADM_CONFIG" --userid "$user_id" --new-password "$INITIAL_ADMIN_PASSWORD" >/dev/null
fi
rm -f "$KCADM_CONFIG"

cat >/etc/backlog-killer/api.env <<EOF
APP_ENV=production
DEPLOYMENT_MODE=$DEPLOYMENT_MODE
LOCAL_DEMO_SEED=false
DATABASE_URL=postgres://backlog_runtime:$POSTGRES_PASSWORD@127.0.0.1:5432/$POSTGRES_DB?sslmode=disable
MIGRATION_DATABASE_URL=postgres://$POSTGRES_MIGRATION_USER:$POSTGRES_MIGRATION_PASSWORD@127.0.0.1:5432/$POSTGRES_DB?sslmode=disable
OIDC_ISSUER=http://127.0.0.1:8081/realms/$OIDC_REALM
OIDC_PUBLIC_ISSUER=https://$OIDC_PUBLIC_HOST/realms/$OIDC_REALM
OIDC_AUDIENCE=$OIDC_AUDIENCE
OIDC_CLIENT_ID=$OIDC_CLIENT_ID
OIDC_CLIENT_SECRET=
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=$KEYCLOAK_ADMIN_PASSWORD
API_PUBLIC_URL=https://$API_PUBLIC_HOST
WEB_PUBLIC_URL=https://$PANEL_PUBLIC_HOST
EMBED_SIGNING_KEY=$EMBED_SIGNING_KEY
STORAGE_DRIVER=filesystem
STORAGE_PATH=/var/lib/backlog-killer/uploads
STORAGE_PROJECT_QUOTA_BYTES=$STORAGE_PROJECT_QUOTA_BYTES
MCP_ALLOWED_ORIGINS=https://$PANEL_PUBLIC_HOST
PLATFORM_ADMIN_SUBJECTS=https://$OIDC_PUBLIC_HOST/realms/$OIDC_REALM|$user_id
EOF
chmod 0600 /etc/backlog-killer/api.env
cat >/etc/systemd/system/backlog-api.service <<'EOF'
[Unit]
Description=Backlog Killer API
After=network-online.target postgresql.service backlog-keycloak.service
Wants=network-online.target
Requires=postgresql.service backlog-keycloak.service

[Service]
Type=simple
User=backlog
Group=backlog
WorkingDirectory=/opt/backlog-killer/current
EnvironmentFile=/etc/backlog-killer/api.env
ExecStart=/opt/backlog-killer/current/server
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/backlog-killer/uploads

[Install]
WantedBy=multi-user.target
EOF

log 'Configuring Caddy and TLS hosts'
cat >/etc/caddy/Caddyfile <<EOF
{
  email $ACME_EMAIL
}

$APP_DOMAIN {
  respond "Landing page is not published yet." 404
}

$SDK_PUBLIC_HOST {
  root * /opt/backlog-killer/current/sdk
  header Access-Control-Allow-Origin "*"
  header X-Content-Type-Options "nosniff"
  header Cache-Control "public, max-age=300"
  file_server
}

$PANEL_PUBLIC_HOST {
  root * /opt/backlog-killer/current/web
  encode zstd gzip
  header X-Content-Type-Options "nosniff"
  header Referrer-Policy "no-referrer"
  header Content-Security-Policy "default-src 'self'; connect-src 'self' https://$API_PUBLIC_HOST https://$OIDC_PUBLIC_HOST; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; frame-ancestors 'none'"
  try_files {path} /index.html
  file_server
}

$API_PUBLIC_HOST {
  encode zstd gzip
  reverse_proxy 127.0.0.1:8080
}

$OIDC_PUBLIC_HOST {
  encode zstd gzip
  reverse_proxy 127.0.0.1:8081
}
EOF
caddy validate --config /etc/caddy/Caddyfile
systemctl daemon-reload
systemctl enable --now backlog-api caddy
systemctl restart backlog-api caddy
for _ in {1..60}; do curl -fsS http://127.0.0.1:8080/readyz >/dev/null 2>&1 && break; sleep 2; done
curl -fsS http://127.0.0.1:8080/readyz >/dev/null
sudo -u postgres psql -v ON_ERROR_STOP=1 --set=migration_user="$POSTGRES_MIGRATION_USER" <<'SQL'
SELECT format('ALTER ROLE %I NOCREATEROLE', :'migration_user') \gexec
SQL

cleanup_config
trap - EXIT
log 'Native deployment completed'
