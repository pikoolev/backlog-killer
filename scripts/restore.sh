#!/bin/sh
set -eu

: "${BACKUP_PATH:?BACKUP_PATH required}"
: "${POSTGRES_MIGRATION_USER:=backlog_migrator}"
: "${POSTGRES_DB:=backlog_killer}"
: "${KEYCLOAK_DB_USER:=keycloak}"
: "${KEYCLOAK_DB_NAME:=keycloak}"

test -f "$BACKUP_PATH/application.dump"
test -f "$BACKUP_PATH/keycloak.dump"
test -f "$BACKUP_PATH/uploads.tar.gz"
(cd "$BACKUP_PATH" && sha256sum -c SHA256SUMS)
uploads_volume="${COMPOSE_PROJECT_NAME:-backlog-killer}_uploads"

resume_services() {
  docker compose start keycloak api >/dev/null 2>&1 || true
}
trap resume_services EXIT HUP INT TERM

docker compose stop api keycloak
docker compose exec -T postgres pg_restore --clean --if-exists --no-owner --no-privileges -U "$POSTGRES_MIGRATION_USER" -d "$POSTGRES_DB" < "$BACKUP_PATH/application.dump"
docker compose exec -T keycloak-postgres pg_restore --clean --if-exists --no-owner --no-privileges -U "$KEYCLOAK_DB_USER" -d "$KEYCLOAK_DB_NAME" < "$BACKUP_PATH/keycloak.dump"
MSYS_NO_PATHCONV=1 docker run --rm -i -v "$uploads_volume:/target" alpine:3.21.3 sh -c 'find /target -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar -C /target -xzf -' < "$BACKUP_PATH/uploads.tar.gz"

resume_services
trap - EXIT HUP INT TERM
printf 'restore complete: %s\n' "$BACKUP_PATH"
