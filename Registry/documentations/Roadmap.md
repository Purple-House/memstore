

# AgniStack Registry Service Implementation Roadmap

### **Phase 1: Prototype**

* Implement single-node in-memory registry with gRPC API.  
* CRUD operations for agents/gateways.  
* WAL-based persistence (no replication).  
* Benchmark local latency (p50/p99).  

### **Phase 2: Partitioned Single-Node Cluster**

* Introduce consistent hashing and partition routing logic.  
* Multiple partitions per node.  
* Snapshot + TTL expiry.  

### **Phase 3: Distributed Cluster**

* Add Raft-based replication (etcd/raft or raft-rs).  
* Implement leader election, replica sync, and snapshot streaming.  
* Add cluster join/rebalance protocols.  

### **Phase 4: Optimization & Hardening**

* Leader lease-based reads.  
* Binary serialization and zero-copy decoding.  
* Detailed metrics, monitoring, and admin tooling.  
* Fault injection tests (node kill, network partition).  

### **Phase 5: Production**

* Multi-region replication.  
* Automated rebalancing.  
* Fine-grained ACLs.  
* Integration with AgniStack control plane.



Gateway Register message 

```bash

{
    "region": "global",
    "gateway_ip": "12.34.56.78",
    "gateway_domain": "global.gateway.agnistack.online",
    "gateway_port": 4000,
    "capacity": {
        "cpu": 1,
        "bandwidth": 20,
        "memory": 4096,
        "storage": 40960
    }
}

```


```bash
WAL reacord encodign format

+-------------+
|  MAGIC (2)  |   = 0xCAFE
+-------------+
| VERSION (1) |
+-------------+
| OP CODE (1) |
+-------------+
| LENGTH (4)  |  protobuf payload length
+-------------+
| PAYLOAD     |  protobuf WalRecord
+-------------+
| CRC32 (4)   |
+-------------+

[magic:2] [version:1] [op:1] [length:4] [payload:N] [crc32:4]


Append logic

┌──────────────────────────┐
│ Append WAL (sync flush)  │
└──────┬───────────────────┘
       ▼
┌──────────────────────────┐
│ Update MemStore in RAM   │
└──────────────────────────┘


Replay logic

Read snapshot (if exists)
Replay WAL from beginning:
    Decode record
    Validate CRC
    Apply to MemStore

```