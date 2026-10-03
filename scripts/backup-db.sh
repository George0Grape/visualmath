#!/usr/bin/env bash
# Ежедневный бэкап SQLite-базы VisualMath.
# Использует команду .backup — она безопасна для живой БД в режиме WAL
# (в отличие от простого cp, который может скопировать несогласованное состояние).
set -euo pipefail

APP_DIR="/home/nikitin/website_visualmath/visualmath"
DB="$APP_DIR/visualmath.db"
BACKUP_DIR="/home/nikitin/website_visualmath/backups"
RETENTION_DAYS=14
TS="$(date +%Y%m%d-%H%M%S)"
DEST="$BACKUP_DIR/visualmath-$TS.db"
LOG="$BACKUP_DIR/backup.log"

mkdir -p "$BACKUP_DIR"

# Консистентный снимок через online-backup API SQLite
sqlite3 "$DB" ".backup '$DEST'"

# Сжимаем (140 МБ → десятки МБ)
gzip -f "$DEST"

# Чистим бэкапы старше RETENTION_DAYS дней
find "$BACKUP_DIR" -name 'visualmath-*.db.gz' -mtime +"$RETENTION_DAYS" -delete

echo "$(date '+%Y-%m-%d %H:%M:%S') OK -> ${DEST}.gz ($(du -h "${DEST}.gz" | cut -f1))" >> "$LOG"
