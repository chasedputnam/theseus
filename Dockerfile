# syntax=docker/dockerfile:1

# Build stage — hardened minimal Go image (no shell)
FROM ghcr.io/rtvkiz/minimal-go:latest AS builder

ARG VERSION=dev

WORKDIR /build

# Set build environment via go env (no shell required)
ENV CGO_ENABLED=0
ENV GOOS=linux

# Copy go mod files first for layer caching
COPY go.mod go.sum ./
RUN ["go", "mod", "download"]

# Copy source
COPY . .

# Build — GOARCH is inherited from the platform docker buildx selects
RUN ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", "theseus", "./cmd/theseus"]

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata tmux bash python3

WORKDIR /app

COPY --from=builder /build/theseus .
COPY --from=builder /build/static ./static

RUN mkdir -p /app/data

EXPOSE 7000

ENV DATA_DIR=/app/data
ENV STATIC_DIR=/app/static
ENV PORT=7000

ENTRYPOINT ["/app/theseus"]
