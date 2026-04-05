BINARY   := coo-agent
MODULE   := github.com/mathiasb/coo-agent
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags="-X main.version=$(VERSION)"

# Default: build for current platform
.PHONY: build
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/coo-agent

# Cross-compilation targets
.PHONY: build-linux-amd64
build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-amd64 ./cmd/coo-agent

.PHONY: build-linux-arm64
build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-arm64 ./cmd/coo-agent

.PHONY: build-darwin-arm64
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-arm64 ./cmd/coo-agent

# Build for all deployment targets
.PHONY: build-all
build-all: build-linux-amd64 build-linux-arm64 build-darwin-arm64
	@echo "Built:"
	@ls -lh bin/

# Deploy targets (requires SSH access)
.PHONY: deploy-koala
deploy-koala: build-linux-amd64
	scp bin/$(BINARY)-linux-amd64 koala:~/.local/bin/$(BINARY)
	ssh koala "systemctl --user restart $(BINARY) || true"

.PHONY: deploy-piguard
deploy-piguard: build-linux-arm64
	scp bin/$(BINARY)-linux-arm64 piguard:~/.local/bin/$(BINARY)
	ssh piguard "systemctl --user restart $(BINARY) || true"

.PHONY: deploy-iguana
deploy-iguana: build-darwin-arm64
	scp bin/$(BINARY)-darwin-arm64 iguana:~/.local/bin/$(BINARY)
	ssh iguana "launchctl kickstart -k gui/$$(ssh iguana id -u)/$(BINARY) || true"

# Testing
.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: test-verbose
test-verbose:
	go test -race -v -count=1 ./...

.PHONY: coverage
coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Code quality
.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: vet
vet:
	go vet ./...

# OAuth2 setup helper – opens the Fortnox authorization URL
.PHONY: auth
auth:
	go run ./cmd/coo-agent -auth

.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html

.PHONY: tidy
tidy:
	go mod tidy
