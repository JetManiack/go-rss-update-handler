# syntax=docker/dockerfile:1

# Build stage runs natively on the build host and cross-compiles to the target
# platform (CGO is disabled, so this is a pure Go cross-compile — no QEMU).
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /app/gruh ./cmd/gruh

FROM alpine:3.22
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S gruh && adduser -S gruh -G gruh
WORKDIR /app
COPY --from=builder --chown=gruh:gruh /app/gruh .
# Writable directory for the default sqlite DSN (data/gruh.db).
RUN mkdir -p /app/data && chown -R gruh:gruh /app
USER gruh
# webui (:8080) and metrics/health (:9090)
EXPOSE 8080 9090
ENTRYPOINT ["./gruh"]
