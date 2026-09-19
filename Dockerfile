# G.E.A.R. deployable image (Story 7.6, NFR-R1): ONE image that serves BOTH
# the API (/api/*) and the built SPA (/, from GEAR_WEB_DIST) — no Vite process
# at runtime. Multi-stage: (1) build web/dist with Node+Vite, (2) build the Go
# server with the `postgres` build tag, (3) minimal runtime image.

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

# --- Stage 3: minimal runtime -----------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=go-build /out/gear-server /app/gear-server
COPY --from=web-build /src/web/dist /app/web/dist
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