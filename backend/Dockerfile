# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM golang:1.24-alpine AS build

WORKDIR /src

# go.mod on its own, ahead of the sources, so the dependency layer is only
# invalidated by a dependency change rather than by every edit to a .go file.
# There is no go.sum: this module has no third-party dependencies yet, so the
# download is a no-op today and this stays correct once that changes.
COPY go.mod ./
RUN go mod download

COPY . .

# CGO off makes the binary static, so the runtime stage doesn't need a libc
# matching the builder's. -trimpath keeps build paths out of the binary and
# -s -w drop the symbol and DWARF tables — together roughly a third off the
# size, at the cost of gdb output nobody reads in production anyway.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server

# ---- runtime --------------------------------------------------------------
# Alpine rather than distroless or scratch. Distroless would be a few MB
# smaller and has no shell, which is the stronger position on paper — but no
# shell also means no HEALTHCHECK and no `docker exec` when the thing is
# misbehaving on the VM at an inconvenient hour. For a single-VM POC that
# trade isn't worth it. Revisit if this ever runs somewhere with a real
# orchestrator doing the health probing.
FROM alpine:3.21

# Copied from the builder rather than installed with `apk add ca-certificates`
# so the runtime stage pulls in no packages at all. The backend needs these to
# reach api.steampowered.com over TLS — without them every Steam call fails
# with x509: certificate signed by unknown authority.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Fixed UID/GID rather than whatever the next free one happens to be, so a
# bind-mounted host directory has predictable ownership across rebuilds.
# -S is a system account, -D has no password, -H skips creating a home dir
# this process never uses.
RUN addgroup -g 10001 -S fragvault \
 && adduser -u 10001 -S -G fragvault -H -D fragvault \
 && mkdir -p /data \
 && chown fragvault:fragvault /data

COPY --from=build /out/server /usr/local/bin/fragvault-server

# The JSON store is the only thing this process writes, and store.go writes it
# via a .tmp file next to it and a rename — so both paths land in /data and
# nothing else on the filesystem is ever written to. That's what lets compose
# run this container with read_only: true.
ENV STORE_PATH=/data/fragvault-data.json \
    LISTEN_ADDR=:8080

VOLUME ["/data"]

USER fragvault:fragvault
EXPOSE 8080

# --spider fetches headers only and writes no file, which matters when the
# root filesystem is read-only. BusyBox wget has no --fail equivalent, but it
# already exits non-zero on a 4xx/5xx.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/fragvault-server"]
