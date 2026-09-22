# G.E.A.R. deployable image (Story 7.6, NFR-R1): ONE image that serves BOTH
# the API (/api/*) and the built SPA (/, from GEAR_WEB_DIST) — no Vite process
# at runtime. Multi-stage: (1) build web/dist with Node+Vite, (2) build the Go
# server with the `postgres` build tag, (3) minimal runtime image.
#
# The runtime gains the postgres client `pg_dump` (Story 7.7, NFR-R3): the
# in-process backup job shells out to it. postgres:18 ships on Debian 13
# (trixie) but the distroless runtime is Debian 12 (bookworm), so trixie
# binaries/libs are NOT ABI-compatible — the `pg-client` stage uses the
# bookworm variant (postgres:18-bookworm) and copies pg_dump + its shared
# library dependencies (ldd) into the runtime, which is switched to
# distroless `base-debian12` (it carries glibc + the dynamic loader that a
# dynamically-linked pg_dump needs; the Go server stays CGO_ENABLED=0 static).

# --- Stage 1: build the SPA -------------------------------------------------
FROM docker.io/library/node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Stage 2: build the Go server -------------------------------------------
FROM docker.io/library/golang:1.27-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The binary is built into the image (not the repo root) — `server` is gitignored.
RUN CGO_ENABLED=0 GOOS=linux go build -tags postgres -trimpath -o /out/gear-server ./cmd/server

# --- Stage 2b: the postgres client (pg_dump) for the backup job ---------------
# Bookworm-built pg_dump 18 (SAME major as the db container) + its shared libs,
# copied into the runtime's native paths so no LD_LIBRARY_PATH trick is needed.
FROM docker.io/library/postgres:18-bookworm AS pg-client
RUN mkdir -p /pgclient/usr/bin && \
    cp /usr/lib/postgresql/18/bin/pg_dump /pgclient/usr/bin/pg_dump && \
    for lib in $(ldd /usr/lib/postgresql/18/bin/pg_dump | awk '$3 ~ /^\// {print $3}'); do \
      mkdir -p "/pgclient$(dirname "$lib")"; cp "$lib" "/pgclient$lib"; \
    done

# --- Stage 3: minimal runtime -----------------------------------------------
FROM gcr.io/distroless/base-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=go-build /out/gear-server /app/gear-server
COPY --from=web-build /src/web/dist /app/web/dist
COPY --from=pg-client /pgclient/ /
# GEAR_WEB_DIST default matches the copy target above (config default ./web/dist).
ENV GEAR_WEB_DIST=/app/web/dist \
    GEAR_HTTP_ADDR=:8080
EXPOSE 8080
USER nonroot
# distroless has no shell/wget: the HEALTHCHECK is the app's own -healthcheck
# probe (exec-form JSON; no shell operators — a `|| exit 1` would be invalid
# here and is unnecessary since the probe already exits 0/1).
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/app/gear-server", "-healthcheck"]
ENTRYPOINT ["/app/gear-server"]