FROM cgr.dev/chainguard/nginx:latest

COPY web/nginx.conf /etc/nginx/conf.d/default.conf
COPY web/dist /usr/share/nginx/html

EXPOSE 8080
