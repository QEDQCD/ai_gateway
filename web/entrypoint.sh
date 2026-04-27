#!/bin/sh
set -eu

SOURCE_FILE="/run/secrets/web_console.htpasswd"
TARGET_FILE="/etc/nginx/web_console.htpasswd"

if [ ! -f "${SOURCE_FILE}" ]; then
  echo "missing htpasswd secret: ${SOURCE_FILE}" >&2
  exit 1
fi

cp "${SOURCE_FILE}" "${TARGET_FILE}"
chown nginx:nginx "${TARGET_FILE}"
chmod 640 "${TARGET_FILE}"

exec nginx -g 'daemon off;'
