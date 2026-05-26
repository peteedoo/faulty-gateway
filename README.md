# Faulty Gateway

A lightweight reverse proxy with rate limiting and health check aggregation for the Faulty Link stack.

## Features

- **Path-based routing** — routes `/api/bridge`, `/api/dashboard`, `/api/brief` to respective backends
- **Token bucket rate limiting** — per-IP, configurable rate and burst
- **Health check aggregation** — calls all backend `/health` endpoints, returns unified status
- **Zero dependencies** — Go standard library only
- **Single binary** — static binary, Docker image available

## Quick Start

```bash
cd ~/faulty-gateway
go run .
```

The gateway listens on `:8888` and routes:
- `/api/bridge/*` → `http://localhost:8080`
- `/api/dashboard/*` → `http://localhost:3336`
- `/api/brief/*` → `http://localhost:3337`
- `/` → `http://localhost:3337` (daily-brief default)

## Health Check

```bash
curl http://localhost:8888/health
```

Returns aggregated health from all backends:
```json
{
  "status": "healthy",
  "timestamp": "2026-05-26T08:00:00Z",
  "services": {
    "/api/bridge": "healthy",
    "/api/dashboard": "healthy",
    "/api/brief": "healthy"
  }
}
```

## Rate Limiting

Default: 10 requests/second per IP, burst of 20.

Returns `429 Too Many Requests` when exceeded:
```json
{"error":"rate limit exceeded"}
```

## Docker

```bash
docker build -t faulty-gateway:latest .
docker run -p 8888:8888 faulty-gateway:latest
```

## Tests

```bash
go test -v ./...
```

## Why This Exists

Instead of managing three separate ports (8080, 3336, 3337), the gateway provides a single entrypoint. It adds rate limiting for public exposure and health aggregation for load balancer integration.
