# Build stage
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go.mod and go.sum files (if they exist)
COPY go.* ./

# Download dependencies (if go.mod exists)
RUN if [ -f go.mod ]; then go mod download; fi

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Final stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose the port the app runs on
EXPOSE 8080

# Run the application
CMD ["./main"] 