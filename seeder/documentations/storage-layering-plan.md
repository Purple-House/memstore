# Storage Layering Plan — Seeder

## Context: Where We Are Now

The current Phase 1 architecture is **pure in-memory** with a WAL as the only durability mechanism.

```
Write path:  gRPC handler → WAL.Append (flush to disk) → MemStore.Add
Read path:   gRPC handler → MemStore lookup (RAM)
Restart:     WAL.Replay → rebuild MemStore from scratch
```

This works well for a single node with a small registry, but has two concrete problems:

1. **Replay cost grows linearly** — every restart must scan and replay the entire WAL from byte 0 to rebuild RAM state. As the log grows, startup latency grows with it.
2. **No snapshot baseline** — the WAL never rotates (by design, Phase 1). There is no point-in-time frozen state to fall back to. If the WAL is corrupted past a certain offset, all state after that offset is lost.

---

## The Three-Layer Plan

The goal is to introduce a **disk store** as a middle tier between the WAL and RAM, keeping reads in memory and writes durable, without discarding what already works.

```
┌─────────────────────────────────────────────────────────────────┐
│                          SEEDER PROCESS                         │
│                                                                 │
│  ┌──────────────────┐                                           │
│  │   MemStore (RAM) │  ◄── All reads served from here          │
│  │   region maps    │      O(1) hash lookups                   │
│  │   B-tree ranking │      No lock contention from disk I/O    │
│  └────────┬─────────┘                                           │
│           │  write-through on every mutation                    │
│           ▼                                                     │
│  ┌──────────────────┐                                           │
│  │  Disk Store      │  ◄── Persistent structured storage       │
│  │  (key/value)     │      Survives process death              │
│  │  per-region DBs  │      Used as snapshot baseline           │
│  └────────┬─────────┘                                           │
│           │  append-only log of every mutation                  │
│           ▼                                                     │
│  ┌──────────────────┐                                           │
│  │   WAL (wal.log)  │  ◄── Source of truth for ordering        │
│  │   magic+CRC32    │      Integrity-checked records           │
│  │   append only    │      Rotates after disk-store flush      │
│  └──────────────────┘                                           │
└─────────────────────────────────────────────────────────────────┘
```

### Layer Responsibilities

| Layer | Medium | Purpose | Read? | Write? |
|---|---|---|---|---|
| **MemStore** | RAM | Sub-millisecond reads; B-tree ranking | Yes (all reads) | Derived from other layers |
| **Disk Store** | Disk (KV) | Durable snapshot of live registry state | On startup only | Write-through on every mutation |
| **WAL** | Disk (log) | Ordered, integrity-checked mutation log | During replay only | On every mutation (before disk store) |

---

## Write Path (New)

Every mutation (RegisterGateway, RegisterAgent, etc.) follows this strict sequence:

```
1.  Compute SHA256 identity
2.  WAL.Append(record)          ← flush to disk first (durability guarantee)
3.  DiskStore.Put(key, value)   ← update persistent snapshot
4.  MemStore.Add(data)          ← update hot cache
```

If the process dies between steps 2 and 3: WAL replay fills the gap on next startup.  
If the process dies between steps 3 and 4: startup loads from disk store directly, no replay needed for that record.

---

## Startup Sequence (New)

```
1.  Load config + TLS
2.  Open Disk Store
3.  Load all disk store records → populate MemStore  (fast, structured reads)
4.  Open WAL; seek to last-flushed-offset stored in disk store metadata
5.  Replay only WAL entries written AFTER the last disk-store flush
6.  Start gRPC + HTTP servers
```

This replaces the current "replay WAL from byte 0" approach. Startup time is bounded by disk store read speed, not WAL length.

---

## Read Path (Unchanged)

All reads continue to be served exclusively from MemStore in RAM. The disk store is **never read on the hot path**. This preserves the current low-latency guarantee.

```
ResolveGatewayForAgent  →  MemStore.GetTopKGateways  (RAM, microseconds)
ResolveGatewayForProxy  →  MemStore.GetAgent         (RAM, microseconds)
```

---

## WAL Rotation

With a disk store as the baseline, the WAL can now be safely rotated:

```
Trigger:  WAL size > 32 MB  (maxWalBytes, currently commented out)
Process:
  1. Flush all pending WAL writes
  2. Record current WAL offset in disk store metadata
  3. Rename wal.log → wal.log.1
  4. Open a new wal.log
  5. Old wal.log.1 can be deleted after next successful disk-store flush
```

This prevents unbounded WAL growth and makes Phase 2 snapshots straightforward.

---

## Disk Store — Technology Options

Three candidate backends, all embed cleanly into a Go binary:

| Option | Library | Notes |
|---|---|---|
| **Pebble** | `cockroachdb/pebble` | LSM-tree, same format as RocksDB. Best write throughput. Used in CockroachDB. |
| **bbolt** | `etcd-io/bbolt` | B-tree, single file, ACID. Simpler ops model. Used in etcd. |
| **BadgerDB** | `dgraph-io/badger` | LSM + value log separation. Good for large values. |

**Recommendation for Phase 2: bbolt**  
- Single `.db` file per region — maps cleanly to the current `map[string]*MemData` region partitioning  
- Read-optimised (B-tree) — aligns with the read-heavy access pattern of startup loading  
- Zero external dependencies, pure Go, battle-tested in etcd  
- ACID transactions make the write-through pattern safe without extra locking

---

## Key Schema (bbolt example)

```
Database file:  seeder-<region>.db

Bucket: "gateways"
  Key:   <GatewayID>  (32-byte hex SHA256)
  Value: proto-encoded GatewayData

Bucket: "agents"
  Key:   <AgentID>
  Value: proto-encoded AgentData

Bucket: "seeders"
  Key:   <SeederID>
  Value: proto-encoded SeederData

Bucket: "meta"
  Key:   "wal_offset"
  Value: uint64 big-endian  (last WAL byte offset fully flushed to this DB)
```

---

## What Changes in the Codebase

| File | Change |
|---|---|
| `wal/wal.go` | Add `CurrentOffset() int64` method; enable WAL rotation logic |
| `wal/replay.go` | Accept a start-offset parameter to replay from a given byte position |
| `pkg/memstore/mem.go` | Add `DiskStore` field to `MemStore`; inject on construction |
| `pkg/memstore/gatewayops.go` | After WAL append, call `diskStore.PutGateway(region, data)` |
| `pkg/memstore/agentsops.go` | After WAL append, call `diskStore.PutAgent(region, data)` |
| `cmd/main.go` | Startup sequence: open disk store → load into mem → partial WAL replay |
| `pkg/diskstore/` *(new)* | `diskstore.go` — Open/Put/Get/LoadAll per entity type; wraps bbolt |

No changes to `pkg/maps/` (gRPC handlers) or `pkg/api/` — the storage layers are below them.

---

## Phase Alignment

| Phase | Storage State |
|---|---|
| **Phase 1 (current)** | RAM + WAL only. Replay from byte 0 on startup. |
| **Phase 2 (this plan)** | RAM + Disk Store (bbolt) + WAL. Partial replay. WAL rotation. |
| **Phase 3** | Per-partition disk stores. Snapshot streaming for Raft replica sync. |
| **Phase 4** | Compaction, zero-copy decoding, disk store metrics. |

---

## Summary

The plan keeps the current fast read path (everything in RAM) and the current WAL integrity model (CRC32, magic bytes, append-only). It adds a structured disk store as a **durable snapshot layer** that:

- Eliminates full WAL replay on every startup
- Enables WAL rotation without losing history
- Provides a clean foundation for Phase 3 snapshotting and replication
- Requires no changes to gRPC handlers or the HTTP viewer
