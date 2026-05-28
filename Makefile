.PHONY: all build clean start test

# Default: build
all: build

# Build the Go processor binary. goldflags.sh stamps version/commit/branch/date
# into the binary (run from repo root so it can read the local git checkout).
build:
	cd processor && go build -ldflags="$$(../scripts/goldflags.sh)" -o poracle-processor ./cmd/processor

# Remove build artifacts
clean:
	rm -f processor/poracle-processor

# Start the processor
start: build
	./start.sh

# Run all tests
test:
	cd processor && go test ./...
