# Development Guide

> Opinionated workflow for this specific project, this specific situation.

---

## Mindset First

**Three rules that prevent project abandonment:**

1. **Working > perfect.** A demo that runs beats elegant code that doesn't. Ship each phase before polishing it.
2. **Commit every green state.** If it compiles and one thing works, commit it. This gives you a safe rollback and visible progress.
3. **Reduce scope, not effort.** When blocked for >30 minutes, cut the feature down — don't power through bad design.

**What "done" looks like for each phase:**

| Phase | Done = |
|---|---|
| 0 (cleanup) | `git status` is clean, CI passes |
| 1 (delete old) | `go build ./...` succeeds |
| 2 (proto) | `make proto` generates valid Go, no compile errors |
| 3 (backend) | `curl /healthz` → 200, events pushed → SSE result appears |
| 4 (frontend) | Browser open → click simulate → results appear in 5s |
| 5 (deploy) | Public URL works end-to-end |

---

## Tool Stack & When to Use Each

### Claude Code (Claude Pro — use heavily in the first 7 days)

Your primary driver for this project. Use it for:
- **Multi-file changes** — proto → generated code → store → aggregator → server wiring
- **Architecture decisions** — "how should FetchWindow interact with the aggregator?"
- **Debugging** — paste the error, let it trace the root cause
- **Code review** — `/review` before committing anything non-trivial

**How to get the best results:**
- CLAUDE.md is already in the repo — Claude auto-loads it. Keep it updated as architecture evolves.
- Be specific: don't say "implement the aggregator," say "implement `internal/stream/aggregator.go` with the tumbling window logic described in CLAUDE.md, using `EventStore.FetchWindow`"
- One task at a time. Don't chain 3 features into one prompt.
- After each phase, do a quick `/review` of the diff before committing.

**7-day strategy:** Front-load the hardest work. Phases 1–3 (backend) are Go-heavy and architecturally complex. Blast through them while Claude Pro is active. The frontend (Phase 4) is more mechanical and Copilot handles it well.

---

### GitHub Copilot (VS Code — always available)

Use for **inline completion** while you're actively typing:
- Filling in repetitive structs, switch cases, error handling boilerplate
- Completing test table rows once you've written the first one
- Autocompleting import paths and method signatures

Use slash commands in the Copilot chat panel:
- `/explain` — understand a block of code you didn't write
- `/fix` — quick targeted bug fixes
- `/tests` — generate test skeletons for a function

**Limitation:** Copilot doesn't know the full repo context well. Use it for local, self-contained completions. Use Claude for anything that touches multiple files.

---

### Google Gemini (student pro — always available)

Use for **research and understanding**, not code generation:
- "How does Redis ZSet scoring interact with ZRANGEBYSCORE when scores are equal?"
- "What's the difference between tumbling and sliding windows in stream processing?"
- "Explain Qwen API rate limits and how to handle 429s"
- Reading and summarizing long documentation pages

Best workflow: Gemini explains the concept → you design the approach → Claude implements it.

---

### Codex (free plan — fallback when Claude quota hits)

Use when Claude Pro quota runs out and you need file-level code generation:
- Provide the function signature + docstring in the prompt
- Give it the interface it needs to implement (paste `EventStore` interface, for example)
- One file per prompt — it loses context across files

**Template for Codex prompts:**
```
Language: Go 1.25
File: internal/stream/aggregator.go
Package: stream

Implement Aggregator.Run() which:
- Ticks every windowSize duration
- For each videoID in activeVideos, calls store.FetchWindow(ctx, videoID, fromMs, toMs)
- If events returned, calls ai.Summarize() and broadcaster.Broadcast()
- Updates lastFlushMs[videoID] = toMs

Dependencies (already defined):
- EventStore interface: PushEvent, FetchWindow
- ai.Client.Summarize(ctx, videoID, texts []string) (emotionTag, coreTopic string, err error)
- SSEBroadcaster.Broadcast(WindowResult)
```

---

## Development Workflow (Per Session)

```
START SESSION
  ↓
Read CLAUDE.md + check git log (what was last working state?)
  ↓
Pick ONE concrete task from the current phase
  ↓
Implement with Claude Code (or Copilot for small fills)
  ↓
Run: go build ./... (or npm run build for frontend)
  ↓
Run: make test (or targeted: go test -run TestXxx ./internal/...)
  ↓
If green → git commit
If red  → debug (max 30 min), then reduce scope if stuck
  ↓
END SESSION: always leave on a green commit
```

**Session length:** 1–3 hours max. Longer sessions produce worse code and more abandonment risk. Stop at a green state.

---

## Phase Execution Order (Strict)

Don't skip ahead. Each phase builds on the previous one compiling cleanly.

```
Phase 0 ✓ → Phase 1 → Phase 2 → Phase 3a → 3b → 3c → 3d → 3e → 3f → 3g → Phase 4 → Phase 5
```

Within Phase 3, the sub-order matters:
```
3a (Redis store) → 3b (AI client) → 3c (aggregator) → 3d (gRPC service) → 3e (SSE) → 3f (HTTP routes) → 3g (main.go wiring)
```

Reason: each step depends on the interface defined in the step before it. Don't implement the aggregator before the store interface exists.

---

## Keeping Scope Ruthlessly Small

The plan has ambitious "future directions." Ignore them until after the professor email.

**For the demo, only these matter:**
- Events pushed to Redis ✓
- Window aggregated by LLM ✓
- Result visible in browser via SSE ✓

Cut anything that isn't on that list. Specifically, defer:
- Authentication / user management
- Multiple video_id concurrency (single video is fine for demo)
- Sliding windows
- Prometheus metrics
- Redis Streams migration

If a feature isn't needed for the demo to work end-to-end, it goes in a GitHub Issue, not in the code.

---

## Interacting With Claude Code Efficiently

**Good prompts (specific, bounded):**
> "Implement `internal/storage/redis/store.go`. It must satisfy the `EventStore` interface in `internal/storage/interface.go`. Use the `luaFetchWindow` script defined in `script.go`. Include table-driven tests."

**Bad prompts (vague, multi-scope):**
> "Implement the storage layer and make it work with the aggregator"

**After Claude makes changes, always:**
1. Read the diff before accepting — Claude sometimes over-engineers
2. Run `go build ./...` immediately
3. If something looks wrong, say "this adds too much complexity, simplify X to just Y"

**Use plan mode** (`/plan`) for any task that touches more than 2 files. It lets you review the approach before any code is written.

---

## When to Use Each Tool by Phase

| Phase | Primary | Secondary |
|---|---|---|
| 1 — delete old modules | Claude Code | — |
| 2 — proto + codegen | Claude Code | Copilot (proto syntax) |
| 3a — Redis store | Claude Code | Gemini (Lua/Redis docs) |
| 3b — AI client | Claude Code | Gemini (Qwen API docs) |
| 3c–3g — wiring | Claude Code | Copilot (boilerplate fills) |
| 4 — React frontend | Copilot (heavy) | Claude Code (SSE hook, layout) |
| 5 — deploy | Claude Code | Gemini (Railway/Render docs) |

---

## Anti-Abandonment Checklist

Before ending any session:
- [ ] `go build ./...` passes (or `npm run build` for frontend work)
- [ ] At least one new commit on this branch
- [ ] The next concrete task is written down (in a comment, issue, or note)

If you feel stuck or unmotivated:
- Switch to a different part of the same phase (e.g., write tests instead of implementation)
- Ship a smaller version of the feature (e.g., hardcoded AI response before real API call)
- Re-read the goal: *a working demo that can be shown to a professor in an email*

---

## Git Conventions (Keep It Simple)

Branch: stay on `fix/ci` through Phase 1-2, then cut `feat/stream-backend` for Phase 3, `feat/frontend` for Phase 4.

Commits: follow existing style — `type(scope): message`
```
feat(stream): implement Redis ZSet event store
feat(ai): add Qwen client with retry backoff
feat(sse): add SSE broadcaster and /stream/results endpoint
fix(aggregator): handle empty window without AI call
```

Don't over-engineer branching. One feature branch per phase is enough.

---

## After the Demo: Personal Brand & Resume

Once the demo works end-to-end:

1. **README** is already written for this — verify it matches reality
2. **Record a 60-second screen capture**: simulate → 5-second window → result appears. This is your portfolio artifact.
3. **GitHub repo public**: make sure it looks clean (no .env, no debug prints, CI green)
4. **Resume bullet**: *"Built real-time danmu stream processing system: Go + Redis ZSet + Qwen LLM + SSE, handling X events/sec with <5s end-to-end latency"*

For long-term maintenance, the project's natural evolution path is already in the plan (Redis Streams, sliding windows, metrics). Each iteration is a concrete engineering improvement you can document — good for continued resume updates and blog posts.
