# syntax=docker/dockerfile:1

# ---------- Build stage ----------
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build a statically linked binary
# TARGETOS/TARGETARCH are set automatically by docker buildx
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /porthole .

# ---------- Runtime stage ----------
FROM alpine:3.21

# ca-certificates needed for HTTPS connections (Elasticsearch etc.)
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /porthole /usr/local/bin/porthole

EXPOSE 8080

ENTRYPOINT ["porthole"]
