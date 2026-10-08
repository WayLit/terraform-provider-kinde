default: testacc

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

# Run unit tests
.PHONY: test
test:
	go test ./... -v $(TESTARGS) -timeout 120s -parallel=4

# Run go fmt against code
.PHONY: fmt
fmt:
	gofmt -w -s .

# Run go vet against code
.PHONY: vet
vet:
	go vet ./...

# Generate documentation
.PHONY: docs
docs:
	tfplugindocs generate --provider-name kinde

# Validate documentation
.PHONY: docs-validate
docs-validate:
	tfplugindocs validate --provider-name kinde

# Run golangci-lint
.PHONY: lint
lint:
	golangci-lint run ./...

# Build provider
.PHONY: build
build: docs-validate
	go build -v ./...

# Install provider
.PHONY: install
install: build
	go install -v ./...

# Clean build artifacts
.PHONY: clean
clean:
	go clean -i ./...

# Run all pre-commit checks
.PHONY: all
all: fmt vet lint docs-validate test

# Version detection
VERSION = $(shell git describe --tags --match 'v*' 2>/dev/null || echo "v0.0.0")

# Test coverage
.PHONY: coverage
coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

.PHONY: default build clean coverage docs docs-validate all
