# ---- Stage 1: Build Go processor ----
FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY processor/go.mod processor/go.sum ./
RUN go mod download

COPY processor/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o poracle-processor ./cmd/processor

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

# Bundled defaults
COPY fallbacks/ fallbacks/

EXPOSE 3030

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://localhost:3030/health || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["./start.sh"]
