FROM redis:7-alpine

WORKDIR /app
COPY dist /app
EXPOSE 5173
CMD ["busybox", "httpd", "-f", "-p", "5173", "-h", "/app"]
