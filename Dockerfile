# Stage 1: Frontend build
FROM node:22-alpine AS frontend-builder

WORKDIR /build/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
# Use vite build directly to skip vue-tsc type checking in Docker
RUN npx vite build


# Stage 2: Go build
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Overwrite the placeholder dist with the real frontend build
COPY --from=frontend-builder /build/web/dist/ ./internal/server/dist/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /flume ./cmd/flume


# Stage 3: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=go-builder /flume /flume

EXPOSE 8080

ENTRYPOINT ["/flume"]
