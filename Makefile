BINARY_NAME := flume
BUILD_DIR   := bin
FRONTEND_DIST := web/dist
EMBED_DIST  := internal/server/dist

.PHONY: build build-frontend build-backend build-collector build-aggregator dev test clean lint proto-gen

build: build-frontend build-backend

build-frontend:
	cd web && npm run build
	rm -rf $(EMBED_DIST)
	cp -r $(FRONTEND_DIST) $(EMBED_DIST)

build-backend:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/flume

build-collector:
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME)-collector ./cmd/flume

build-aggregator:
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME)-aggregator ./cmd/flume

dev-aggregator:
	go run ./cmd/flume aggregator --verbose

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

proto-gen:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/flume/v1/collector.proto
