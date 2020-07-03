FROM alpine:3.11.6
COPY caddy /app/caddy
CMD ["./app/caddy", "run", "--config", "/app/Caddyfile"]

HEALTHCHECK --interval=5s --timeout=10s --start-period=5s \
  CMD curl -fs http://localhost:$PORT/health || exit 1
