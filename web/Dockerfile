FROM nginx:1.27-alpine

COPY web/dist /usr/share/nginx/html
COPY web/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
