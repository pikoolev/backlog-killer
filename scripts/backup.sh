#!/bin/sh
set -eu

: "${BACKUP_DIR:?BACKUP_DIR required}"
: "${POSTGRES_MIGRATION_USER:=backlog_migrator}"
: "${POSTGRES_DB:=backlog_killer}"
: "${KEYCLOAK_DB_USER:=keycloak}"
: "${KEYCLOAK_DB_NAME:=keycloak}"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="$BACKUP_DIR/$stamp"
uploads_volume="${COMPOSE_PROJECT_NAME:-backlog-killer}_uploads"
mkdir -p "$target"

resume_services() {
  docker compose start keycloak api >/dev/null 2>&1 || true
}
trap resume_services EXIT HUP INT TERM

# Quiesce every writer before dumping the two databases and attachment volume.
docker compose stop api keycloak
docker compose exec -T postgres pg_dump -U "$POSTGRES_MIGRATION_USER" -Fc "$POSTGRES_DB" > "$target/application.dump"
docker compose exec -T keycloak-postgres pg_dump -U "$KEYCLOAK_DB_USER" -Fc "$KEYCLOAK_DB_NAME" > "$target/keycloak.dump"
MSYS_NO_PATHCONV=1 docker run --rm -v "$uploads_volume:/source:ro" alpine:3.21.3 tar -C /source -czf - . > "$target/uploads.tar.gz"
(
  cd "$target"
  sha256sum application.dump keycloak.dump uploads.tar.gz > SHA256SUMS
)

resume_services
trap - EXIT HUP INT TERM
printf '%s\n' "$target"
