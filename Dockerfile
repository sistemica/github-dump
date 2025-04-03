FROM golang:1.23-alpine AS builder

# Install git and build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o github-dump

# Use a small alpine image for the final image
FROM alpine:3.19

# Install required runtime dependencies
RUN apk add --no-cache git tree

# Copy the binary from the builder stage
COPY --from=builder /app/github-dump /usr/local/bin/

# Create directories for temp files
RUN mkdir -p /tmp/repos /tmp/output

# Set the working directory
WORKDIR /app

# Expose the default port
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["github-dump"]