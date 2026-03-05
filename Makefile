BINARY_NAME := flume
BUILD_DIR   := bin
FRONTEND_DIST := web/dist
EMBED_DIST  := internal/server/dist

.PHONY: build build-frontend build-backend dev clean lint test

build: build-frontend build-backend

build-frontend:
	cd web && npm run build
	rm -rf $(EMBED_DIST)
	cp -r $(FRONTEND_DIST) $(EMBED_DIST)

build-backend:
	GOROOT=/usr/lib/go-1.24 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/flume

dev:
	GOROOT=/usr/lib/go-1.24 go run ./cmd/flume

test:
	GOROOT=/usr/lib/go-1.24 go test ./internal/...

clean:
	rm -rf $(BUILD_DIR)
	rm -rf $(FRONTEND_DIST)
	rm -rf $(EMBED_DIST)

lint:
	GOROOT=/usr/lib/go-1.24 go vet ./...
	golangci-lint run ./...
