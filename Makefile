.PHONY: all build clean start test

# Default: build
all: build

# Build the Go processor binary
build:
	cd processor && go build -o poracle-processor ./cmd/processor

# Remove build artifacts
clean:
	rm -f processor/poracle-processor

# Start the processor
start: build
	./start.sh

# Run all tests
test:
	cd processor && go test ./...
