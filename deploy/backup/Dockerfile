# Backup and restore tools: pg_dump/pg_restore matching the server's major
# version, plus rclone for S3-compatible storage (Cloudflare R2, MinIO).
ARG PG_MAJOR=16
FROM postgres:${PG_MAJOR}-alpine
RUN apk add --no-cache rclone
COPY backup.sh restore.sh lib.sh /usr/local/bin/
ENTRYPOINT []
CMD ["backup.sh"]
