# ==========================================
# Stage 1: Build Frontend Assets
# ==========================================
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# ==========================================
# Stage 2: Build Go Binary with Embedded UI
# ==========================================
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS backend-builder
WORKDIR /app/backend

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# Install build dependencies
RUN apk add --no-cache git ca-certificates

COPY backend/go.* ./
RUN go mod download

COPY backend/ ./
# Copy built frontend dist into backend internal static embed dir
COPY --from=frontend-builder /app/frontend/dist ./internal/api/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT#v} \
    go build -ldflags="-s -w" -o /dockerpulse ./cmd/dockerpulse

# ==========================================
# Stage 3: Minimal Production Image
# ==========================================
FROM alpine:3.20

# Install Docker CLI, Docker Compose plugin, and ca-certificates
RUN apk add --no-cache \
    docker-cli \
    docker-cli-compose \
    ca-certificates \
    tzdata \
    bash

WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /dockerpulse /usr/local/bin/dockerpulse

# Default data directory
RUN mkdir -p /data /root/docker

ARG GITHUB_REPOSITORY=farmers00/dockerpulse
ENV GITHUB_REPOSITORY=$GITHUB_REPOSITORY
ENV DATA_DIR=/data

EXPOSE 8080

ENTRYPOINT ["dockerpulse"]
CMD ["server", "--port", "8080", "--data-dir", "/data"]
