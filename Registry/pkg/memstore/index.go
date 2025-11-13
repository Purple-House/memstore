package memstore

import "github.com/google/btree"

func NewMemStore() *MemStore {
	return &MemStore{
		regions: make(map[string]*MemData),
		global:  newMemData(),
	}
}

func newMemData() *MemData {
	return &MemData{
		Gateways: make(map[string]*GatewayData),
		Agents:   make(map[string]*AgentData),
		ranked:   btree.New(2),
	}
}

func (c Capacity) Rank() float64 {
	return float64(c.CPU) + float64(c.Memory)/1024 + float64(c.Storage)/10240 + float64(c.Bandwidth)/1024
}

type GatewayRankItem struct {
	Rank float64
	ID   string
}

func (a *GatewayRankItem) Less(b btree.Item) bool {
	if a.Rank == b.(*GatewayRankItem).Rank {
		return a.ID < b.(*GatewayRankItem).ID
	}
	return a.Rank < b.(*GatewayRankItem).Rank
}
