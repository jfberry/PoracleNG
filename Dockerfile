# ---- Stage 1: Build Go processor ----
FROM golang:1.26-alpine AS go-builder

WORKDIR /build
COPY processor/go.mod processor/go.sum ./
RUN go mod download

COPY processor/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o poracle-processor ./cmd/processor

# ---- Stage 2: Install Node.js alerter dependencies ----
FROM node:24-alpine AS node-builder

RUN apk add --no-cache python3 make g++ git

WORKDIR /build
COPY alerter/package.json ./
RUN npm install --omit=dev --ignore-scripts && npm rebuild

# ---- Stage 3: Runtime image ----
FROM node:24-alpine

RUN apk add --no-cache curl bash tini tzdata

WORKDIR /app

# Processor binary
COPY --from=go-builder /build/poracle-processor processor/poracle-processor

# Alerter source + dependencies
COPY alerter/ alerter/
COPY --from=node-builder /build/node_modules alerter/node_modules

# Shared files
COPY config/config.example.toml config/config.example.toml
COPY start.sh start.sh
RUN chmod +x start.sh processor/poracle-processor

# Create runtime directories
RUN mkdir -p config/.cache/geofences resources/data resources/locale alerter/logs alerter/nominatimData logs backups

# Processor: 3030, Alerter: 3031
EXPOSE 3030 3031

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://localhost:3030/health || exit 1

ENTRYPOINT ["tini", "--"]
CMD ["./start.sh"]
