.PHONY: all build build-processor install-alerter clean start test

# Default: build everything
all: build

# Build both components
build: build-processor install-alerter

# Build the Go processor binary
build-processor:
	cd processor && go build -o processor ./cmd/processor

# Install alerter Node.js dependencies
install-alerter:
	cd alerter && npm install

# Remove build artifacts
clean:
	rm -f processor/processor

# Start both components via start.sh
start: build
	./start.sh

# Run processor tests
test:
	cd processor && go test ./...
