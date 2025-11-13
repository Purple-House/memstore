package memstore

import "github.com/google/btree"

func (mem *MemStore) AddGateway(region string, gateway *GatewayData) GatewayData {
	data := mem.RegionExist(region)

	data.mu.Lock()
	defer data.mu.Unlock()

	gatewayData, exist := data.Gateways[gateway.GatewayDomain]
	if exist {
		// Remove old rank item
		oldRank := gatewayData.Capacity.Rank()
		data.ranked.Delete(&GatewayRankItem{
			Rank: oldRank,
			ID:   gateway.GatewayDomain,
		})

		return *gatewayData
	}
	data.Gateways[gateway.GatewayDomain] = gateway
	data.ranked.ReplaceOrInsert(&GatewayRankItem{
		Rank: gateway.Capacity.Rank(),
		ID:   gateway.GatewayDomain,
	})

	return *gateway
}

func (mem *MemStore) GetTopKGateways(region string, k int) []*GatewayData {
	data := mem.RegionExist(region)

	data.mu.RLock()
	defer data.mu.RUnlock()

	var result []*GatewayData
	count := 0
	data.ranked.Ascend(func(item btree.Item) bool {
		if count >= k {
			return false
		}
		gi := item.(*GatewayRankItem)
		result = append(result, data.Gateways[gi.ID])
		count++
		return true
	})
	return result
}

func (mem *MemStore) GetGateway(region, gatewayDomain string) (*GatewayData, bool) {
	data := mem.RegionExist(region)

	data.mu.RLock()
	defer data.mu.RUnlock()
	gateway, exist := data.Gateways[gatewayDomain]
	return gateway, exist
}
