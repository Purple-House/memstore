package maps

import (
	memstore "github.com/dipghoshraj/ingress-tunnel/registry/pkg/memstore"
	mapper "github.com/dipghoshraj/ingress-tunnel/registry/proto"
)

type RPCMap struct {
	mapper.UnimplementedMapsServer
	MemStore *memstore.MemStore
}

var _ mapper.MapsServer = (*RPCMap)(nil)

func NewRPCMap() *RPCMap {
	return &RPCMap{
		MemStore: memstore.NewMemStore(),
	}
}
