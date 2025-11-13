package memstore

import "fmt"

func (mem *MemStore) AddAgent(region string, agent *AgentData) (*AgentData, *GatewayData, error) {
	data := mem.RegionExist(region)

	data.mu.Lock()
	defer data.mu.Unlock()

	gateway, exist := mem.GetGateway(region, agent.GatewayDomain)
	if !exist {
		return &AgentData{}, nil, fmt.Errorf("gateway %s not found in region %s", agent.GatewayDomain, region)
	}

	_, exist = data.Agents[agent.AgentDomain]
	if exist {
		fmt.Printf("Agent %s already exists in region %s\n", agent.AgentDomain, region)
	}
	data.Agents[agent.AgentDomain] = agent
	return agent, gateway, nil
}
