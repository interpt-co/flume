BINARY_NAME := flume
BUILD_DIR   := bin
FRONTEND_DIST := web/dist
EMBED_DIST  := internal/server/dist

.PHONY: build build-frontend build-backend build-collector build-dispatcher dev test clean lint

build: build-frontend build-backend

build-frontend:
	cd web && npm run build
	rm -rf $(EMBED_DIST)
	cp -r $(FRONTEND_DIST) $(EMBED_DIST)

build-backend:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/flume

build-collector:
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME)-collector ./cmd/flume

build-dispatcher:
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME)-dispatcher ./cmd/flume

dev-dispatcher:
	go run ./cmd/flume dispatcher --verbose --redis-addr localhost:6379

dev-collector:
	go run ./cmd/flume collector --config test-config.yaml --verbose

test:
	go test ./internal/...

clean:
	rm -rf $(BUILD_DIR)
	rm -rf $(FRONTEND_DIST)
	rm -rf $(EMBED_DIST)

lint:
	go vet ./...
	golangci-lint run ./...
