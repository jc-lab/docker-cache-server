FROM golang:1.23-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o docker-cache-server ./cmd/server

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/docker-cache-server .

# Copy example config
COPY config.example.yaml .

# Create data directory
RUN mkdir -p /var/cache/docker-cache-server

# Expose default port
EXPOSE 5000

# Volume for persistent storage
VOLUME ["/var/cache/docker-cache-server"]

ENTRYPOINT ["./docker-cache-server"]
CMD ["--config", "config.example.yaml"]