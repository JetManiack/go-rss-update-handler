FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gruh ./cmd/gruh

FROM alpine:latest
RUN apk --no-cache add ca-certificates && \
    addgroup -S gruh && adduser -S gruh -G gruh
WORKDIR /app
COPY --from=builder /app/gruh .
RUN chown -R gruh:gruh /app
USER gruh
ENTRYPOINT ["./gruh"]
