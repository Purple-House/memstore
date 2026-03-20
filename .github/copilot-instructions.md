# Memstore Seeder — Project Guidelines

## Overview

**Seeder** is a gRPC-based in-memory registry service for AgniStack ingress tunnels. It stores Gateways, Agents, and Seeders partitioned by region, backed by a write-ahead log (WAL) for crash recovery. Phase 1: single-node, no replication.

All source lives under `seeder/`. The binary is `seeder/bin/seeder`.

## Build & Run

All commands run from `seeder/`:

```bash
make install-deps   # go mod tidy
make build          # build for current OS → bin/seeder[.exe]
make build-all      # cross-compile Linux / macOS / Windows
make run            # go run cmd/main.go
make proto-gen      # regenerate gRPC stubs from proto/maps.proto
make wal-proto-gen  # regenerate WAL protobuf code
make gen-cert       # generate self-signed TLS certificates
```

**Go 1.25+ required.** Module: `github.com/odio4u/memstore/seeder`.

There are no unit test targets yet — when adding tests, use `go test ./...` from `seeder/`.

## Architecture

```
seeder/
  cmd/main.go          ← startup: config → TLS → WAL replay → gRPC server
  pkg/
    memstore/          ← thread-safe in-memory store (RWMutex + per-region Mutex)
      mem.go           ← MemStore struct, region map, B-tree for gateway ranking
      agentsops.go     ← AddAgent / GetAgent
      gatewayops.go    ← AddGateway / GetTopKGateways / GetGateway
      seederops.go     ← AddSeeder / GetSeeders
      index.go         ← shared types: AgentData, GatewayData, SeederData
    maps/              ← gRPC handlers (RPCMap embedded in UnimplementedMapsServer)
      rpc.go           ← RegisterMapsServer glue
      gateway.go       ← RegisterGateway RPC
      agent.go         ← RegisterAgent RPC
      poll.go          ← ResolveGatewayForAgent / ResolveGatewayForProxy RPCs
    api/
      api.go           ← HTTP GET /seeder?region=<name> viewer endpoint (port 9000)
  wal/
    wal.go             ← Append (write path): protobuf → CRC32 → flush to disk
    replay.go          ← Replay (read path): validate magic/CRC32, call apply callback
```

**Protobuf schemas** live in the external `github.com/odio4u/agni-schema` module — do not edit generated `*.pb.go` files directly; run `make proto-gen` / `make wal-proto-gen` instead.

## Key Conventions

### Data model
- Data is partitioned by **region** string (e.g., `"global"`, `"us-east-1"`).
- Every registration derives a **deterministic SHA256 identity**:
  ```go
  id := hex.EncodeToString(sha256.Sum256([]byte(credHash + "|" + field))[:])
  ```
- Gateway **capacity ranking** uses a B-tree score:
  ```
  Rank = CPU + Memory/1024 + Storage/10240 + Bandwidth/1024
  ```

### Write path (gRPC → WAL → MemStore)
Every mutable RPC (RegisterGateway, RegisterAgent) must:
1. Compute SHA256 identity
2. Call `WALer.Append(record)` — synchronous flush
3. Write to MemStore

### Error handling
Return `mapper.Error` (from the agni-schema package) in gRPC responses — not raw Go errors.

### Thread safety
- Region map: guarded by `MemStore.mu` (sync.RWMutex)
- Per-region operations: guarded by `MemData.mu` (sync.Mutex)
- Never acquire a region lock while holding the store-level lock

### TLS
- Minimum TLS 1.3, cipher suites: `TLS_AES_128_GCM_SHA256`, `TLS_AES_256_GCM_SHA384`
- Session tickets disabled; renegotiation never
- SHA256 certificate fingerprint is logged on startup — clients must verify it

## Configuration

`seeder-config.yaml` (in the working directory):

```yaml
version: v1
Seeder:
  name: "seedercert"   # TLS cert CN
  dns: "localhost"     # DNS SAN
  ip: "127.0.0.1"      # bind address
  port: 8080           # gRPC port (defaults to 50051 if empty)
  viewer: 9000         # HTTP viewer port
  region: "global"     # region identifier
```

## Roadmap & Constraints (Phase 1)

- Single node, no replication or consensus
- WAL never rotates — no snapshots yet (planned Phase 2)
- B-tree gateway ranking is in-memory only — no persistence beyond WAL replay
- `pkg/api/` is a read-only debug viewer, not a production API
