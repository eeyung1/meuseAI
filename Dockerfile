# Build Stage
FROM golang:1.24-alpine AS builder

# Install build dependencies for CGO (sqlite3)
RUN apk add --no-cache build-base git ca-certificates

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Build binary
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /bin/meuseai .

# Runtime Stage
FROM alpine:3.21

# Install CA certificates and timezone data for outbound HTTPS and scheduling
RUN apk add --no-cache ca-certificates tzdata

# Create dedicated unprivileged system user
RUN addgroup -S meuseai && adduser -S meuseai -G meuseai

WORKDIR /home/meuseai

# Copy binary from builder
COPY --from=builder /bin/meuseai /bin/meuseai

# Switch to unprivileged user
USER meuseai

# Expose default port
EXPOSE 8080

ENTRYPOINT ["/bin/meuseai"]
