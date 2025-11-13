package memstore

import (
	"sync"

	"github.com/google/btree"
)

type Resource string

const (
	ResourceGateway Resource = "Gateway"
	ResourceAgent   Resource = "Agent"
)

type MemStore struct {
	mu      sync.RWMutex
	regions map[string]*MemData
	global  *MemData
}

type MemData struct {
	Gateways map[string]*GatewayData
	Agents   map[string]*AgentData
	ranked   *btree.BTree
	mu       sync.RWMutex
}

type AgentData struct {
	AgentID       string
	AgentDomain   string
	GatewayID     string
	GatewayDomain string
}

type GatewayData struct {
	GatewayID     string
	GatewayIP     string
	GatewayDomain string
	Capacity      Capacity
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
			mem.regions[region] = &MemData{
				Gateways: make(map[string]*GatewayData),
				Agents:   make(map[string]*AgentData),
				ranked:   btree.New(2),
			}
		}
		data = mem.regions[region]
		mem.mu.Unlock()
	}
	return data

}
