# AGENTS.md

Go client library (`github.com/taigrr/signalcli`) providing typed bindings for
[signal-cli](https://github.com/AsamK/signal-cli)'s JSON-RPC HTTP API. This is a
single-package library at the repo root — there is no `cmd/`, no `main`.

## Commands

```bash
go test -race -count=1 ./...   # tests (CI runs on go 1.26)
go vet ./...
staticcheck ./...              # CI lint; install: go install honnef.co/go/tools/cmd/staticcheck@latest
```

CI (`.github/workflows/ci.yml`) runs on push/PR to `master`. There is no
Makefile. Only external dep is `github.com/google/uuid`.

## Layout

- `client.go` — `Client`, JSON-RPC transport (`Call`), and all request methods
  (Send, React, SendTyping, MarkRead, GetProfile, ListGroups, ListContacts,
  UpdateProfile, SetExpiration, Block/Unblock, etc.).
- `sse.go` — `Listener` for receiving messages via Server-Sent Events, plus all
  inbound message type structs (`Envelope`, `DataMessage`, `SyncMessage`, ...).
- `daemon.go` — `Daemon`, optional manager that spawns/monitors a `signal-cli`
  subprocess in daemon mode.
- `watchdog.go` — memory watchdog on `Daemon`: `MemoryUsage`, `Restart`,
  `Watch`/`WatchConfig`. signal-cli (JVM) leaks memory over time; `Watch`
  samples RSS and restarts the daemon when it stays over a limit.
- `*_test.go` — each source file has a sibling test.

## Architecture / data flow

- **Outbound**: `Client.Call` POSTs a `RPCRequest` (JSON-RPC 2.0, random uuid
  ID) to `baseURL + /api/v1/rpc`. Higher-level methods build a
  `map[string]interface{}` of params (NOT the exported `...Params` structs — the
  structs document the API but methods hand-build the map, injecting `account`).
- **Inbound**: `Listener.Listen` opens SSE at `baseURL + /api/v1/events?account=`
  and auto-reconnects every 5s on error until ctx is cancelled. `readEvents`
  hand-parses the SSE wire format (not a library).

## Non-obvious conventions / gotchas

- **Endpoints**: RPC is `/api/v1/rpc`; events are `/api/v1/events` (a past bug
  fixed the events path — don't change it).
- **Wrapped-vs-direct responses**: signal-cli sometimes returns a result
  directly and sometimes wrapped in `{"response": ...}`. `Send` and
  `handleEvent` try direct unmarshal first, then fall back to the wrapped shape.
  Follow this pattern for new response parsing.
- **React param name mismatch**: `ReactParams.TargetTimestamp` maps to the RPC
  field `targetSentTimestamp` (see `React`). Struct json tags are not always the
  wire names because the map is built manually.
- **Empty-value omission**: methods only add map keys when values are non-empty
  (e.g. `if params.GroupID != ""`), so zero values are never sent.
- **SSE**: no read timeout (`Timeout: 0`); 1MB max line via `scanner.Buffer`;
  handler errors are intentionally swallowed to keep the stream alive.
- **Daemon** defaults to `127.0.0.1:8080`, always passes `--no-receive-stdout`,
  and treats *any* HTTP response (even 400) as "reachable"; if the daemon is
  already up externally, `Start` just marks running without spawning.
- **Lossless restart**: `Stop` sends SIGINT and waits (via the `done` channel)
  for the process to fully exit so signal-cli flushes local state; messages that
  arrive while down are queued by Signal servers and redelivered on reconnect,
  and a `Listener` auto-reconnects. `monitor` owns the sole `cmd.Wait()` — never
  call `cmd.Wait()` elsewhere; wait on `done` instead.
- **MemoryUsage** is platform-split via build tags: `memory_linux.go` reads
  `/proc/<pid>/statm` (resident pages × pagesize), `memory_darwin.go` shells out
  to `ps -o rss=` (KB ×1024), and `memory_other.go` (`!linux && !darwin`) returns
  an unsupported error (so `Watch` is a no-op restarter on Windows). All keep
  the repo dep-free. `watchdog.go` calls the shared `sampleRSS(ctx, pid)`.
- **JVM heap cap**: `DaemonConfig.JavaMaxHeapMB` appends `-Xmx<n>m` to the
  subprocess `JAVA_OPTS` (via `buildEnv`, preserving existing JAVA_OPTS). If
  left 0, `Watch` auto-derives it as ¾ of `MemoryLimit` so the JVM trips the
  restart before exhausting host memory. Set it explicitly to also cap the
  initial process (Watch only affects restarts).

## Testing pattern

Tests spin up `httptest.NewServer`, assert the inbound JSON-RPC body, and return
canned responses — no real signal-cli needed. Match this when adding methods.

## Style

Standard gofmt. Keep it clean under `staticcheck`. Code uses `any` (not
`interface{}`) and `strings.CutPrefix`; match that.
