#!/bin/sh
set -eu

if [ -z "${GATEWAY_SERVICE_AUTH_USERNAME:-}" ] || [ -z "${GATEWAY_SERVICE_AUTH_PASSWORD:-}" ]; then
  echo "missing gateway service auth credentials for web proxy" >&2
  exit 1
fi

cat >/etc/nginx/conf.d/default.conf <<EOF
server {
    listen 8080;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    location /api/ {
        proxy_pass http://gateway:8080/;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header X-Service-User "${GATEWAY_SERVICE_AUTH_USERNAME}";
        proxy_set_header X-Service-Password "${GATEWAY_SERVICE_AUTH_PASSWORD}";
        proxy_set_header X-Console-Session \$http_x_console_session;
        proxy_set_header X-Console-Subject "";
    }

    location / {
        try_files \$uri \$uri/ /index.html;
    }
}
EOF

exec nginx -g 'daemon off;'
