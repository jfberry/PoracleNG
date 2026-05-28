# ---- Stage 1: Build Go processor ----
FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY processor/go.mod processor/go.sum ./
RUN go mod download

COPY processor/ ./

# Git metadata for the !version command. Go can't auto-embed VCS info here
# because .git is not in the build context (it lives at the repo root and is
# .dockerignore'd), so inject it via ldflags from build args. The CI workflow
# passes the commit SHA and build date; bare `docker build` falls back to the
# "unknown" defaults below, leaving the static version.go constant intact.
ARG VERSION_COMMIT=""
ARG VERSION_BRANCH=""
ARG VERSION_DATE=""
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w \
      -X github.com/pokemon/poracleng/processor.Commit=${VERSION_COMMIT} \
      -X github.com/pokemon/poracleng/processor.Branch=${VERSION_BRANCH} \
      -X github.com/pokemon/poracleng/processor.Date=${VERSION_DATE}" \
    -o poracle-processor ./cmd/processor

# ---- Stage 2: Runtime image ----
FROM alpine:3

RUN apk add --no-cache curl bash tini tzdata

WORKDIR /app

# Processor binary
COPY --from=builder /build/poracle-processor processor/poracle-processor
RUN chmod +x processor/poracle-processor

# Shared files
COPY config/config.example.toml config/config.example.toml
COPY start.sh start.sh
RUN chmod +x start.sh

# Scripts for PoracleJS migration
COPY scripts/ scripts/

# Create runtime directories (processor downloads resources at startup)
RUN mkdir -p config/.cache/geofences resources/rawdata resources/gamelocale logs backups

# Pre-packaged game data (util.json is also embedded in the Go binary,
# but some code paths read it from disk)
COPY resources/data/util.json resources/data/util.json

# Bundled defaults (util.json is now embedded in the Go binary)
COPY fallbacks/ fallbacks/

# Bind to all interfaces inside the container so docker-compose port
# mapping (and bridge networking) can reach the processor. The code
# default is 127.0.0.1 for bare-metal safety; this env var flips it
# without requiring users to touch config.toml.
ENV PORACLE_HOST=0.0.0.0

EXPOSE 3030

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://localhost:3030/health || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["./start.sh"]
