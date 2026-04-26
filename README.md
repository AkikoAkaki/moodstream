# Danmu Sentiment Aggregator

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Redis](https://img.shields.io/badge/Redis-7.x-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A real-time stream processing platform that ingests high-concurrency danmu (bullet comment) streams, performs LLM-based accessibility analysis using tumbling-window aggregation, and pushes results to clients via Server-Sent Events.

Built on Go + Redis ZSet + Lua atomic operations. Originally a distributed delay queue MVP; refactored into an HCI-oriented interactive log processing system.

## Architecture

```
[React Frontend]
  ├── Left panel:  Danmu injection (POST /events/push)
  └── Right panel: SSE results    (GET /stream/results)
              ↕ SSE
[Go Server]
  ├── gRPC Server (:9090)  → StreamService.PushEvents (Client Streaming)
  │     └── writes to Redis ZSet: stream:{video_id}:events (score=timestamp_ms)
  ├── HTTP Server (:8080)
  │     ├── POST /events/push    → JSON ingestion for frontend
  │     └── GET  /stream/results → SSE broadcast endpoint
  └── Aggregator goroutine (tumbling window, 5s)
        ├── Lua atomic fetch: ZRANGEBYSCORE + ZREMRANGEBYSCORE
        ├── Calls LLM API (Qwen / OpenAI-compatible)
        └── Broadcasts WindowResult to all SSE subscribers
[Redis]
  └── stream:{video_id}:events — ZSet (score = video timestamp_ms)
```

**Data flow**:
1. Client streams `InteractionEvent` (video_id, timestamp_ms, raw_text) via gRPC or HTTP POST
2. Events land in Redis ZSet keyed by video playback position
3. Every 5 seconds, a Lua script atomically fetches the current window's events and removes them (no duplicate processing)
4. LLM extracts `emotion_tag` + `core_topic` from the batch
5. `WindowResult` is broadcast to all connected SSE clients in real time

## Quick Start

### Prerequisites
- Go 1.25+
- Docker / Docker Desktop
- `make`
- `grpcurl` (optional, for manual testing)

### Run

```bash
# Start Redis
make up

# Start server (gRPC :9090 + HTTP :8080)
make run-server
```

### Test the pipeline

```bash
# Health check
curl http://localhost:8080/healthz

# Push a danmu event
curl -X POST http://localhost:8080/events/push \
  -H 'Content-Type: application/json' \
  -d '{"video_id":"v1","timestamp_ms":3000,"raw_text":"这里好好笑哈哈哈"}'

# Stream results (blocks, open in separate terminal)
curl -N http://localhost:8080/stream/results

# gRPC service listing
grpcurl -plaintext localhost:9090 list
```

### Frontend

> **Note:** The React frontend (`web/`) and `api/proto/stream.proto` are under active development (Phase 4). The backend server and Redis pipeline are functional independently.

```bash
cd web && npm install && npm run dev
# Open http://localhost:5173
```

## Development

```bash
make test        # run all tests with race detector
make lint        # golangci-lint
make proto       # regenerate protobuf from api/proto/stream.proto
make build-server
```

## Config

Loaded from `./config.yaml` or `./config/config.yaml`. Override via env vars with prefix `DDQ_`:

| Env Var | Description |
|---|---|
| `DDQ_REDIS_ADDR` | Redis address (default `localhost:6379`) |
| `DDQ_AI_API_KEY` | LLM API key (Qwen / OpenAI-compatible) |
| `DDQ_AI_BASE_URL` | LLM base URL |
| `DDQ_STREAM_WINDOW_SIZE_SECONDS` | Aggregation window size (default `5`) |

## License

MIT License. See `LICENSE`.
