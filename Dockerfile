FROM golang:1.14.2 as builder
WORKDIR /app
COPY . /app
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o caddy cmd/main.go


FROM alpine:3.11.6
COPY --from=builder /app/caddy /app/caddy
CMD ["./caddy" "run", "--config", "Caddyfile"]

HEALTHCHECK --interval=5s --timeout=10s --start-period=5s \
  CMD curl -fs http://localhost:$PORT/health || exit 1
