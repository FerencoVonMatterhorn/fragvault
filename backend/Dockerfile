# Build a static Go binary, then ship it on a near-empty base. Mirrors
# frontend/Dockerfile: nothing from the build stage survives into the runtime
# image except the binary itself.

FROM golang:1.24-alpine AS build
WORKDIR /src

# go.mod is the entire dependency story — this backend deliberately has no
# third-party dependencies (there is no go.sum), so this is a formality that
# keeps the layer cached rather than a real download.
COPY go.mod ./
RUN go mod download

COPY . .
# CGO off so the binary is static and runs on any base. -trimpath drops local
# build paths; -s -w strips debug info, worth a few MB on a 1 GB VM.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20 AS runtime

# ca-certificates is not optional: the backend calls the Steam Web API over
# HTTPS and would fail certificate verification without it. alpine rather
# than distroless so busybox wget can serve the healthcheck below.
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 fragvault \
    && mkdir -p /data \
    && chown fragvault:fragvault /data

COPY --from=build /out/server /usr/local/bin/server

USER fragvault

# The store is a JSON file holding onboarded users and their Valve auth
# codes. It lives on a volume because otherwise every redeploy silently
# discards everyone's onboarding.
ENV STORE_PATH=/data/fragvault-data.json
VOLUME /data

EXPOSE 8080

# Hits the handler already defined in cmd/server/main.go.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
