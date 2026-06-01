# Build stage
FROM golang:1.22-alpine AS builder

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary — uses TARGETOS/TARGETARCH set by docker buildx for multi-platform builds
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o theseus ./cmd/theseus

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata tmux bash python3

WORKDIR /app

# Copy binary and static assets
COPY --from=builder /build/theseus .
COPY --from=builder /build/static ./static

# Data directory
RUN mkdir -p /app/data

EXPOSE 7000

ENV DATA_DIR=/app/data
ENV STATIC_DIR=/app/static
ENV PORT=7000

ENTRYPOINT ["/app/theseus"]
