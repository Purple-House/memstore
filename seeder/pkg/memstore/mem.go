package memstore

import (
	"sync"
	"time"

	"github.com/google/btree"
)

type Resource string

const (
	ResourceGateway Resource = "Gateway"
	ResourceAgent   Resource = "Agent"
)

type MemStore struct {
	mu          sync.RWMutex
	regions     map[string]*MemData
	global      *MemData
	expiryHeap  *agentExpiryHeap
	expiryMu    sync.Mutex
	expiryReset chan struct{}
	agentTTL    time.Duration
}

type MemData struct {
	Gateways map[string]*GatewayData
	Agents   map[string]*AgentData
	Seeders  map[string]*SeederData
	ranked   *btree.BTree
	Mu       sync.RWMutex
}

type AgentData struct {
	AgentID        string
	AgentDomain    string
	GatewayID      string
	GatewayIP      string
	GatewayPort    int32
	Wssport        int32
	GatewayAddress string
	VerifiableHash string
	LastSeenAt     int64 // Unix timestamp; updated on every RegisterAgent call
}
type SeederData struct {
	SeederID       string
	Name           string
	Dns            string
	SeedIP         string
	SeedPort       string
	Region         string
	VerifiableHash string
}

type GatewayData struct {
	GatewayID      string
	GatewayIP      string
	GatewayAddress string
	GatewayPort    int32
	Wssport        int32
	Capacity       Capacity
	VerifiableHash string
}

type Capacity struct {
	CPU       int32
	Memory    int32
	Storage   int32
	Bandwidth int32
}

func (mem *MemStore) RegionExist(region string) *MemData {
	mem.mu.RLock()
	data, ok := mem.regions[region]
	mem.mu.RUnlock()

	if !ok {
		mem.mu.Lock()
		_, exists := mem.regions[region]
		if !exists {
			mem.regions[region] = newMemData()
		}
		data = mem.regions[region]
		mem.mu.Unlock()
	}
	return data

}
