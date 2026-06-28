# ==============================================================================
# Stage 1: Build the Svelte Playground
# ==============================================================================
FROM node:20-alpine AS frontend-builder

WORKDIR /web
# Install dependencies first for better caching
COPY playground/package*.json ./
RUN npm ci

# Copy the playground code and compile the Svelte client
COPY playground/ ./
RUN npm run build

# ==============================================================================
# Stage 2: Build the Go binary
# ==============================================================================
FROM golang:1.24-alpine AS builder

# Enable automatic toolchain download to satisfy go.mod version
ENV GOTOOLCHAIN=auto

WORKDIR /src

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy the compiled Svelte dist folder from the frontend-builder stage
COPY --from=frontend-builder /internal/playground/dist ./internal/playground/dist

# Compile with maximum optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" \
    -o /bin/clever-ai-gate \
    ./cmd/server

# ==============================================================================
# Stage 3: Minimal production runtime
# ==============================================================================
FROM alpine:3.19

# Install runtime dependencies only
RUN apk add --no-cache ca-certificates tzdata

# Copy the compiled binary
COPY --from=builder /bin/clever-ai-gate /usr/local/bin/clever-ai-gate

# Clever Cloud requires port 8080
EXPOSE 8080

# Production mode
ENV GIN_MODE=release
ENV PORT=8080

# Health check for container orchestration
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

# Run as non-root for security
RUN adduser -D -u 1000 appuser
USER appuser

ENTRYPOINT ["/usr/local/bin/clever-ai-gate"]
