# Seeder — End-to-End Documentation

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Data Model](#data-model)
4. [Configuration](#configuration)
5. [TLS & Security](#tls--security)
6. [Startup Sequence](#startup-sequence)
7. [Write-Ahead Log (WAL)](#write-ahead-log-wal)
8. [In-Memory Store](#in-memory-store)
9. [gRPC API](#grpc-api)
10. [HTTP Viewer API](#http-viewer-api)
11. [Build & Run](#build--run)
12. [Operational Notes](#operational-notes)

---

## Overview

**Seeder** is a gRPC-based in-memory registry service for the AgniStack ingress tunnel infrastructure. It acts as the central discovery and routing table, tracking three entity types:

| Entity | Role |
|---|---|
| **Gateway** | An ingress tunnel endpoint with known capacity. Clients connect through it. |
| **Agent** | A domain-bearing node that lives behind a Gateway. Proxies look up Agents to find their Gateway. |
| **Seeder** | The registry service itself. Multiple Seeder instances can be discovered via this endpoint. |

All state is **partitioned by region**, held in RAM for low-latency reads, and durably recorded to a **Write-Ahead Log** (WAL) for crash recovery. Phase 1 is single-node with no replication.

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                        seeder binary                     │
│                                                          │
│  cmd/main.go ── startup: config → TLS → WAL replay      │
│                                                          │
│  ┌─────────────────────┐   ┌──────────────────────────┐  │
│  │   gRPC server       │   │   HTTP viewer server     │  │
│  │   :8080 (TLS 1.3)   │   │   :9000 (plain HTTP)     │  │
│  │                     │   │                          │  │
│  │  pkg/maps/          │   │  pkg/api/                │  │
│  │   gateway.go        │   │   api.go                 │  │
│  │   agent.go          │   │   GET /seeder?region=    │  │
│  │   poll.go           │   └──────────────────────────┘  │
│  │   rpc.go            │                                  │
│  └────────┬────────────┘                                  │
│           │                                               │
│           ▼                                               │
│  ┌────────────────────┐   ┌──────────────────────────┐   │
│  │  pkg/memstore/     │   │  wal/                    │   │
│  │   mem.go           │   │   wal.go  (Append)       │   │
│  │   index.go         │◄──│   replay.go (Replay)     │   │
│  │   gatewayops.go    │   └──────────────────────────┘   │
│  │   agentsops.go     │              │                    │
│  │   seederops.go     │              ▼                    │
│  └────────────────────┘           wal.log                 │
└──────────────────────────────────────────────────────────┘
```

### Package responsibilities

| Package | Responsibility |
|---|---|
| `cmd/main.go` | Entrypoint. Reads config, loads TLS, opens WAL, replays log, starts both servers. |
| `pkg/memstore` | Thread-safe singleton in-memory store. Region-partitioned maps + B-tree capacity ranking. |
| `pkg/maps` | gRPC handler implementations (`RPCMap` struct embeds `UnimplementedMapsServer`). |
| `pkg/api` | Read-only HTTP debug viewer for Seeder discovery. |
| `wal` | Append-only WAL with magic header + CRC32 integrity checks. |

---

## Data Model

### GatewayData

```
GatewayID       string    // SHA256(VerifiableCredHash | GatewayIP)
GatewayIP       string
GatewayAddress  string    // GatewayIP:GatewayPort (derived)
GatewayPort     int32
Wssport         int32     // WebSocket port
Capacity        Capacity
VerifiableHash  string
```

**Capacity** fields used for ranking:

```
CPU       int32
Memory    int32   // MiB
Storage   int32   // MiB
Bandwidth int32   // Mbps
```

**Rank formula** (higher = better):

$$\text{Rank} = \text{CPU} + \frac{\text{Memory}}{1024} + \frac{\text{Storage}}{10240} + \frac{\text{Bandwidth}}{1024}$$

### AgentData

```
AgentID         string    // SHA256(VerifiableCredHash | AgentDomain)
AgentDomain     string    // keyed on this field in the region map
GatewayID       string    // must reference a registered Gateway
GatewayIP       string    // copied from Gateway at write time
GatewayPort     int32
Wssport         int32
GatewayAddress  string
VerifiableHash  string
```

### SeederData

```
SeederID        string
Name            string    // TLS cert CN
Dns             string
SeedIP          string
SeedPort        string
Region          string
VerifiableHash  string    // SHA256 certificate fingerprint
```

### Identity derivation

Every registration derives a **deterministic, collision-resistant SHA256 identity**:

```go
// Gateway
id = hex( SHA256( verifiableCredHash + "|" + gatewayIP ) )

// Agent
id = hex( SHA256( verifiableCredHash + "|" + agentDomain ) )
```

This means re-registering the same credential+endpoint combo is idempotent — it updates the existing record rather than creating a duplicate.

---

## Configuration

File: `seeder-config.yaml` (must exist in the working directory at startup).

```yaml
version: v1

Seeder:
  name: "seedercert"   # Common Name used when generating TLS certificates
  dns: "localhost"     # DNS Subject Alternative Name (SAN)
  ip: "127.0.0.1"      # Bind address for gRPC and certificate IP SAN
  port: 8080           # gRPC listen port (defaults to 50051 if empty)
  viewer: 9000         # HTTP viewer listen port
  region: "global"     # Region this Seeder instance is registered under
```

All fields are loaded at startup; the process exits fatally if the file is missing or malformed.

---

## TLS & Security

Seeder enforces **TLS 1.3 only** on the gRPC port. No fallback to older versions.

| Setting | Value |
|---|---|
| Min/Max version | TLS 1.3 |
| Cipher suites | `TLS_AES_128_GCM_SHA256`, `TLS_AES_256_GCM_SHA384` |
| Curve preferences | X25519, P-256 |
| Session tickets | Disabled |
| Renegotiation | Never |

The SHA256 fingerprint of `server.pem` is logged to stdout at startup so clients can perform out-of-band certificate pinning:

```
[Agni Seeder] Client CERT fingerprint (SHA256): <hex>
```

### Certificate generation

Certificates are **not** bundled; generate them before first run:

```bash
# From seeder/
make gen-cert
# or
./bin/seeder -gen-cert
```

This calls `pkg.GenerateSelfSignedGPR` from `github.com/odio4u/mem-sdk/certengine`, producing `server.pem` and `server-key.pem` signed for the configured IP and DNS SANs.

---

## Startup Sequence

```
1. Read & parse seeder-config.yaml
2. If -gen-cert flag: generate TLS certs and exit
3. Load TLS key pair (server.pem + server-key.pem)
4. Log SHA256 certificate fingerprint
5. Open WAL file (wal.log) — create if missing
6. Create MemStore singleton
7. Register gRPC server (TLS + panic-recovery interceptors)
8. Replay WAL → restore all Gateways and Agents from disk
9. Start gRPC server (goroutine) on :port
10. Start HTTP viewer server (goroutine) on :viewer
11. Wait for both servers to signal ready
12. Self-register this Seeder into the MemStore
13. Block on SIGINT/SIGTERM → graceful gRPC stop
```

The WAL replay (step 8) happens **before** accepting any connections, so the store is always consistent after a crash restart.

---

## Write-Ahead Log (WAL)

### File format

Each record is a fixed 8-byte header followed by a protobuf payload and a 4-byte CRC32 trailer:

```
Offset  Size  Field
──────  ────  ─────────────────────────────────────
0       2     Magic = 0xCAFE
2       1     Version = 1
3       1     Operation code (walpb.Operation enum)
4       4     Payload length (big-endian uint32)
8       N     Protobuf-encoded walpb.WalRecord
8+N     4     CRC32-IEEE of payload (big-endian uint32)
```

### Write path (`wal.Append`)

Called synchronously inside every mutating gRPC handler **after** the in-memory write:

```
gRPC handler
  └─ MemStore.AddGateway / AddAgent   ← in-memory write first
  └─ WALer.Append(walpb.WalRecord)    ← serialize → CRC32 → buffered flush
```

The WAL write holds an internal mutex; concurrent RPCs queue behind it.

### Replay path (`wal.Replay` + `wal.ApplyRecord`)

On startup, every record is read back:
1. Validate magic bytes.
2. Validate CRC32; return `ErrCorrupt` on mismatch.
3. Unmarshal the `WalRecord` protobuf.
4. Pass to `ApplyRecord`, which re-executes `AddGateway` / `AddAgent` against a fresh `MemStore`.
5. SHA256 identities are **re-derived** from the stored credential hash and endpoint, ensuring the replayed identities are deterministically consistent.

> **Note:** The WAL never rotates or snapshots in Phase 1. It grows unbounded. Snapshot and rotation are planned for Phase 2.

---

## In-Memory Store

### Singleton access

```go
store := memstore.GetMemStore()  // returns global singleton (sync.Once)
store := memstore.NewMemStore()  // used in main.go for explicit lifecycle control
```

### Thread-safety model

Two lock levels exist; they must **never** be acquired in reverse order:

| Lock | Type | Protects |
|---|---|---|
| `MemStore.mu` | `sync.RWMutex` | The `regions` map (region create/lookup) |
| `MemData.Mu` | `sync.RWMutex` | Per-region `Gateways`, `Agents`, `Seeders` maps and the B-tree |

The `RegionExist` helper performs a double-checked lock when creating a new region:

```go
func (mem *MemStore) RegionExist(region string) *MemData {
    // fast path: RLock
    // slow path: Lock → check again → create
}
```

### Gateway capacity ranking (B-tree)

Each region holds a `*btree.BTree` of `GatewayRankItem{Rank float64, ID string}`. On every `AddGateway` call:

1. If the gateway already exists, its old rank item is deleted from the tree.
2. The updated gateway is stored in the map.
3. A new rank item is inserted with the recalculated score.

`GetTopKGateways` does an ascending scan, collecting the first `k` items from the tree and looking up their full records from the map. Lower scores come first in the scan — callers wanting the *best* gateways should treat the last items returned as highest-ranked, or the caller can reverse the slice.

### Operations summary

| Method | Description |
|---|---|
| `AddGateway(region, *GatewayData)` | Upsert gateway; update B-tree rank. |
| `GetGateway(region, id)` | Lookup by gateway ID. |
| `GetTopKGateways(region, k)` | Return up to k gateways ordered by rank. |
| `AddAgent(region, *AgentData)` | Insert agent; requires gateway to exist; copies gateway network details onto the agent record. Re-registers refresh gateway binding. |
| `GetAgent(domain, region)` | Lookup by agent domain; refreshes gateway fields from the live gateway record. |
| `AddSeeder(*SeederData)` | Upsert seeder in its `.Region`. |
| `GetSeeders(region)` | Return up to 5 seeders in a region (map iteration order). |

---

## gRPC API

The gRPC service is defined in `github.com/odio4u/agni-schema/maps` (external schema module). The `RPCMap` struct wires the handlers:

```go
type RPCMap struct {
    mapper.UnimplementedMapsServer
    MemStore *memstore.MemStore
    WALer    *wal.WALer
}
```

gRPC server reflection is enabled (`reflection.Register(s)`), so tools like `grpcurl` can introspect the schema at runtime.

---

### `RegisterGateway`

**Request** (`GatewayPutRequest`):

| Field | Required | Notes |
|---|---|---|
| `gateway_ip` | yes | |
| `gateway_port` | yes | |
| `verifiable_cred_hash` | yes | Used to derive identity |
| `wss_port` | no | WebSocket port |
| `region` | no | Defaults to `"global"` |
| `capacity` | no | CPU / Memory / Storage / Bandwidth |

**Flow:**
1. Validate required fields → return error code `1` on failure.
2. Derive `GatewayID = SHA256(credHash + "|" + ip)`.
3. Call `MemStore.AddGateway`.
4. Append `OP_PUT_GATEWAY` WAL record.
5. Return full `GatewayResponse` including derived address and identity.

**Response** (`GatewayResponse`):

```
gateway_id, gateway_ip, gateway_address, gateway_port, wss_port,
identity (verifiable hash), capacity, error
```

---

### `RegisterAgent`

**Request** (`AgentConnectionRequest`):

| Field | Required | Notes |
|---|---|---|
| `verifiable_cred_hash` | yes | |
| `agent_domain` | yes | Unique key within a region |
| `gateway_id` | yes | Must reference an existing registered Gateway |
| `region` | yes | |

**Flow:**
1. Validate required fields → return error code `1` on failure.
2. Derive `AgentID = SHA256(credHash + "|" + agentDomain)`.
3. Call `MemStore.AddAgent` — this resolves and copies gateway network info onto the agent.
4. Append `OP_PUT_AGENT` WAL record.
5. Return full `AgentResponse` including resolved gateway details.

**Response** (`AgentResponse`):

```
agent_id, agent_domain, gateway_id, gateway_address, gateway_ip,
gateway_port, wss_port, identity, capacity, error
```

---

### `ResolveGatewayForAgent`

**Request** (`GatewayHandshake`): region or session context (schema-defined).

**Flow:** Returns the top 10 gateways from the `"global"` region ranked by capacity. Returns error code `2` if no gateways are registered.

**Response** (`MultipleGateways`): list of `GatewayResponse` + optional `Error`.

---

### `ResolveGatewayForProxy`

**Request** (`ProxyMapping`):

| Field | Notes |
|---|---|
| `agent_domain` | The domain to look up |
| `region` | Region to search |

**Flow:** Calls `MemStore.GetAgent`. If found, returns the agent's current gateway binding. Returns error code `2` if the agent is not registered.

**Response** (`AgentResponse`): same shape as `RegisterAgent` response.

---

### Error model

All RPCs return **application-level errors** inside the response body using `mapper.Error{Code, Message}` rather than gRPC status codes. The transport-level call itself always returns `nil` for the Go error unless a panic is caught by the recovery interceptor.

| Code | Meaning |
|---|---|
| `1` | Invalid request / write failure |
| `2` | Not found |

---

## HTTP Viewer API

A lightweight plain-HTTP server runs on the viewer port (`:9000` by default). It is a **read-only debug interface** — not a production API.

### `GET /seeder?region=<name>`

Returns up to 5 `SeederData` records for the given region as a JSON array.

**Example:**

```bash
curl http://localhost:9000/seeder?region=global
```

**Response:**

```json
[
  {
    "SeederID": "register-q",
    "Name": "seedercert",
    "Dns": "localhost",
    "SeedIP": "127.0.0.1",
    "SeedPort": "8080",
    "Region": "global",
    "VerifiableHash": "<sha256-cert-fingerprint>"
  }
]
```

The Seeder self-registers one record into its own store on every startup, so this endpoint always reflects the live instance.

---

## Build & Run

All commands must be run from the `seeder/` directory.

```bash
# Install / tidy dependencies
make install-deps

# Build for the current OS
make build             # → bin/seeder.exe (Windows) or bin/seeder

# Cross-compile for all targets
make build-all         # → release/seeder-linux-amd64
                       #   release/seeder-macos-amd64
                       #   release/seeder-windows-amd64.exe

# Run directly (no build step)
make run

# Regenerate gRPC stubs from proto/maps.proto
make proto-gen

# Regenerate WAL protobuf code
make wal-proto-gen

# Generate TLS certificates (first-time setup)
make gen-cert
```

**Minimum Go version:** 1.25  
**Module:** `github.com/odio4u/memstore/seeder`

### First-time setup

```bash
cd seeder/
make install-deps
make gen-cert           # creates server.pem and server-key.pem
./bin/seeder            # or: make run
```

### Runtime files expected in the working directory

| File | Source | Purpose |
|---|---|---|
| `seeder-config.yaml` | Repo | Configuration |
| `server.pem` | `make gen-cert` | TLS certificate |
| `server-key.pem` | `make gen-cert` | TLS private key |
| `wal.log` | Auto-created | Write-ahead log |

---

## Operational Notes

### Panic recovery

A `grpc_recovery` unary and stream interceptor wraps all gRPC handlers. Any panic is caught, a full stack trace is logged, and the client receives an `"internal server error"` message. The server stays alive.

### Graceful shutdown

The process blocks on `SIGINT` / `SIGTERM`. On signal receipt, `grpc.Server.Stop()` is called, which waits for in-flight RPCs to drain before exiting. The HTTP viewer server does not participate in the drain.

### WAL limitations (Phase 1)

- The WAL file (`wal.log`) grows indefinitely; there is no size cap, rotation, or compaction.
- Snapshots are not implemented. A very long-running instance will have a proportionally long startup replay time after a restart.
- Both limitations are planned for Phase 2.

### Concurrency considerations

- Each gRPC handler acquires the per-region `MemData.Mu` lock for writes.
- WAL appends hold `WALer.mu`; gRPC write handlers serialize behind this for the append step.
- Read-only handlers (`ResolveGatewayForProxy`, `ResolveGatewayForAgent`) acquire `MemData.Mu` as a read lock, allowing concurrent reads.
- Never acquire `MemStore.mu` while already holding `MemData.Mu`.

### External dependencies

| Module | Purpose |
|---|---|
| `github.com/odio4u/agni-schema` | Shared protobuf schemas for gRPC and WAL records |
| `github.com/odio4u/mem-sdk/certengine` | Self-signed certificate generation |
| `github.com/google/btree` | B-tree for capacity-ranked gateway selection |
| `github.com/gorilla/mux` | HTTP router for the viewer API |
| `github.com/grpc-ecosystem/go-grpc-middleware` | Panic recovery interceptors |
| `google.golang.org/grpc` | gRPC transport |
| `google.golang.org/protobuf` | Protobuf serialization |
| `gopkg.in/yaml.v3` | Configuration file parsing |
