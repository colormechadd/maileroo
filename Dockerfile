# Stage 1: Build
FROM golang:1.25-bookworm AS builder

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Install tailwindcss CLI
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64 && \
    chmod +x tailwindcss-linux-x64 && \
    mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate assets and build
RUN go generate ./...
RUN go build -o mailaroo cmd/mailaroo/*.go

# Stage 2: Runtime
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary and assets
COPY --from=builder /app/mailaroo .
COPY --from=builder /app/static ./static
COPY --from=builder /app/db/migrations ./db/migrations

# Standard SMTP ports + Web UI
EXPOSE 25 2525 8080

# The root command of the binary starts the server by default
ENTRYPOINT ["./mailaroo"]
