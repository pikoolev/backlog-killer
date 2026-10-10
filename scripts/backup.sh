#!/bin/sh
set -eu
: "${BACKUP_DIR:?BACKUP_DIR required}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BACKUP_DIR/$stamp"
docker compose exec -T postgres pg_dump -U "${POSTGRES_MIGRATION_USER:-backlog_migrator}" -Fc "${POSTGRES_DB:-backlog_killer}" > "$BACKUP_DIR/$stamp/database.dump"
docker run --rm -v backlog-killer_uploads:/source:ro -v "$BACKUP_DIR/$stamp:/backup" alpine:3.21.3 tar -C /source -czf /backup/uploads.tar.gz .
sha256sum "$BACKUP_DIR/$stamp/database.dump" "$BACKUP_DIR/$stamp/uploads.tar.gz" > "$BACKUP_DIR/$stamp/SHA256SUMS"
printf '%s\n' "$BACKUP_DIR/$stamp"
