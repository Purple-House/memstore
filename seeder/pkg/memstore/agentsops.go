package memstore

import (
	"fmt"
	"time"
)

func (mem *MemStore) AddAgent(region string, agent *AgentData) (*AgentData, *GatewayData, error) {
	now := time.Now()

	gateway, exist := mem.GetGateway(region, agent.GatewayID)
	if !exist {
		return &AgentData{}, nil, fmt.Errorf("gateway %s not found in region %s", agent.GatewayID, region)
	}

	data := mem.RegionExist(region)

	data.Mu.Lock()

	existing, exist := data.Agents[agent.AgentDomain]
	if exist {
		fmt.Printf("Agent %s already exists in region %s — refreshing TTL\n", agent.AgentDomain, region)
		existing.GatewayID = gateway.GatewayID
		existing.GatewayIP = gateway.GatewayIP
		existing.GatewayAddress = gateway.GatewayAddress
		existing.LastSeenAt = now.Unix()
		data.Mu.Unlock()
		// pushExpiry must be called after releasing data.Mu to avoid lock-order inversion
		// (expiry worker: expiryMu → data.Mu; write path must be: data.Mu → released → expiryMu).
		mem.pushExpiry(agent.AgentDomain, region, now.Add(mem.agentTTL))
		return existing, gateway, nil
	}

	agent.GatewayIP = gateway.GatewayIP
	agent.GatewayAddress = gateway.GatewayAddress
	agent.GatewayPort = gateway.GatewayPort
	agent.Wssport = gateway.Wssport
	agent.LastSeenAt = now.Unix()

	data.Agents[agent.AgentDomain] = agent
	data.Mu.Unlock()

	mem.pushExpiry(agent.AgentDomain, region, now.Add(mem.agentTTL))

	fmt.Println("Added the agent to gateway", agent.AgentID, agent.GatewayID)
	return agent, gateway, nil
}

// DeleteAgent removes an agent record from the given region.
// It is safe to call if the agent has already been removed.
func (mem *MemStore) DeleteAgent(region, agentDomain string) {
	data := mem.RegionExist(region)
	data.Mu.Lock()
	delete(data.Agents, agentDomain)
	data.Mu.Unlock()
}

func (mem *MemStore) GetAgent(agentDomain, region string) (*AgentData, bool) {
	data := mem.RegionExist(region)
	agent, exists := data.Agents[agentDomain]
	if !exists {
		return &AgentData{}, false
	}

	fmt.Println("found the agent ", agent.AgentID)

	gateway, exist := mem.GetGateway(region, agent.GatewayID)
	if exist {
		agent.GatewayIP = gateway.GatewayIP
		agent.GatewayAddress = gateway.GatewayAddress
		agent.GatewayID = gateway.GatewayID
	}

	return agent, exists
}
