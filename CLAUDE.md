# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Start Redis (required before running anything)
make up

# Run the server (gRPC :9090 + HTTP SSE :8080)
make run-server

# Regenerate protobuf code after editing api/proto/*.proto
make proto

# Run all tests (with race detector; integration tests use testcontainers)
make test

# Run a single test
go test -v -run TestFooBar ./internal/storage/redis/

# Lint + format
make lint

# Build binaries to ./bin/
make build-server
```

## Architecture

This is a Go + Redis real-time stream processing platform (formerly a delay queue MVP, now being refactored).

**Data flow**:
1. gRPC Client Streaming (`PushEvents`) ingests `InteractionEvent` structs (video_id + timestamp_ms + raw_text) into Redis ZSet with `timestamp_ms` as score
2. A tumbling-window aggregator goroutine fires every N seconds, atomically fetches events from ZSet via Lua script (`ZRANGEBYSCORE` + `ZREMRANGEBYSCORE`), and sends the batch to the LLM API
3. LLM response (`emotion_tag`, `core_topic`) is serialized as `WindowResult` and broadcast over SSE (`GET /stream/results`)
4. React frontend (in `web/`) injects simulated events on the left panel and receives SSE results on the right

**Redis key scheme** (per video_id):
- `stream:{video_id}:events` — ZSet, score = timestamp_ms, member = JSON-encoded InteractionEvent
- Lua scripts handle atomic fetch-and-delete to prevent duplicate consumption

**Key packages**:
- `internal/stream/` — aggregator (tumbling window) + gRPC streaming service handler
- `internal/ai/` — LLM API client (Qwen / OpenAI-compatible)
- `internal/storage/redis/` — ZSet store + Lua scripts (core reusable layer)
- `internal/conf/` — Viper-based config (env prefix: `DDQ_`, e.g. `DDQ_REDIS_ADDR`)
- `cmd/server/` — single binary: gRPC server + HTTP SSE endpoint

**Proto**: `api/proto/stream.proto` defines `InteractionEvent`, `WindowResult`, `StreamService`.

## Config

Config loads from `./config.yaml` or `./config/config.yaml`, with env var overrides.
Env prefix: `DDQ_`. Key vars: `DDQ_REDIS_ADDR`, `DDQ_AI_API_KEY`, `DDQ_AI_BASE_URL`.
