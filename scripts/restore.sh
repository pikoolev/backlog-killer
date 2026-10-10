#!/bin/sh
set -eu
: "${BACKUP_PATH:?BACKUP_PATH required}"
: "${POSTGRES_MIGRATION_USER:?POSTGRES_MIGRATION_USER required}"
: "${POSTGRES_DB:?POSTGRES_DB required}"
test -f "$BACKUP_PATH/database.dump"
test -f "$BACKUP_PATH/uploads.tar.gz"
(cd "$BACKUP_PATH" && sha256sum -c SHA256SUMS)
docker compose exec -T postgres pg_restore --clean --if-exists --no-owner --no-privileges -U "$POSTGRES_MIGRATION_USER" -d "$POSTGRES_DB" < "$BACKUP_PATH/database.dump"
docker run --rm -v backlog-killer_uploads:/target -v "$BACKUP_PATH:/backup:ro" alpine:3.21.3 sh -c 'rm -rf /target/* && tar -C /target -xzf /backup/uploads.tar.gz'
printf 'restore complete: %s\n' "$BACKUP_PATH"
