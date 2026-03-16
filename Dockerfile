# Stage 1: Build both binaries
FROM golang:1.24-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -o goscribe ./cmd/goscribe && \
    go build -ldflags="-s -w" -o server ./cmd/server

# Stage 2: Minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache ffmpeg ca-certificates

# Non-root user for security
RUN addgroup -S goscribe && adduser -S goscribe -G goscribe

WORKDIR /app

COPY --from=builder /build/goscribe /usr/local/bin/goscribe
COPY --from=builder /build/server   /usr/local/bin/server

RUN mkdir -p /tmp/goscribe-uploads && \
    chown goscribe:goscribe /tmp/goscribe-uploads

USER goscribe

EXPOSE 8080

ENTRYPOINT ["server"]
