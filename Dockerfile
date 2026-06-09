# =============================================================================
# Stage 1: Build the Go binary (Builder Stage)
# =============================================================================
FROM golang:1.22-alpine AS builder

# Install ca-certificates and git in case they are needed for private dependencies
RUN apk add --no-cache ca-certificates git

# Set the working directory inside the container
WORKDIR /app

# Copy dependency manifest files first to leverage Docker layer caching
COPY go.mod go.sum ./

# Download all Go dependencies
RUN go mod download

# Copy the entire source codebase
COPY . .

# Compile the static Go binary
# - CGO_ENABLED=0 disables dynamic linking for a self-contained static binary
# - GOOS=linux compiles specifically for the target Linux container OS
# - -ldflags="-s -w" strips debug symbols to minimize the final binary footprint
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/api ./cmd/api

# =============================================================================
# Stage 2: Final production image (Minimal Base)
# =============================================================================
FROM alpine:3.19

# Add CA certificates and timezone database (crucial for secure emails & accurate logging)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy the statically compiled binary from the builder stage
COPY --from=builder /app/api /app/api

# Copy the .env configuration file from the builder stage
COPY --from=builder /app/.env /app/.env

# Copy the static web assets folder required for static file serving
COPY --from=builder /app/web /app/web

# Expose the application port (8080)
EXPOSE 8080

# Define entrypoint to run the compiled API binary
ENTRYPOINT ["/app/api"]
