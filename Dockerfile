# Multi-stage build for FileBox
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o filebox .

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN adduser -D -s /bin/sh filebox

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/filebox .

# Create files directory
RUN mkdir -p /app/files && chown filebox:filebox /app/files

# Switch to non-root user
USER filebox

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/files || exit 1

# Run the binary
CMD ["./filebox"]
