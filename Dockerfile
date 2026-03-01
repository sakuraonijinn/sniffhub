# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY sniffhub/go.mod sniffhub/go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY sniffhub/. .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sniffhub .

# Final stage
FROM alpine:latest

WORKDIR /root/

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/sniffhub .

# Create necessary directories for the application
RUN mkdir -p /root/dash /root/loot /root/real

# Expose port
EXPOSE 8080

# Run the application
CMD ["./sniffhub"]
