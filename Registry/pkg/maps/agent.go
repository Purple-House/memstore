package maps

import (
	"context"

	memstore "github.com/dipghoshraj/ingress-tunnel/registry/pkg/memstore"
	mapper "github.com/dipghoshraj/ingress-tunnel/registry/proto"
	"github.com/google/uuid"
)

func (rpc *RPCMap) RegisterAgent(ctx context.Context, req *mapper.AgentConnectionRequest) (*mapper.AgentResponse, error) {

	agentData := &memstore.AgentData{
		AgentDomain:   req.AgentDomain,
		AgentID:       uuid.New().String(),
		GatewayDomain: req.Domain,
		GatewayID:     req.GatewayId,
	}

	agent, gateway, err := rpc.MemStore.AddAgent("global", agentData)
	if err != nil {
		return &mapper.AgentResponse{
			Error: &mapper.Error{
				Code:    1,
				Message: err.Error(),
			},
		}, nil
	}
	return &mapper.AgentResponse{
		AgentId:       agent.AgentID,
		AgentDomain:   agent.AgentDomain,
		GatewayDomain: gateway.GatewayDomain,
		GatewayId:     gateway.GatewayID,
		Capacity: &mapper.Capacity{
			Cpu:       gateway.Capacity.CPU,
			Memory:    gateway.Capacity.Memory,
			Storage:   gateway.Capacity.Storage,
			Bandwidth: gateway.Capacity.Bandwidth,
		},
		Error: nil,
	}, nil
}

func (rpc *RPCMap) ResolveGatewayForAgent(ctx context.Context, req *mapper.GatewayHandshake) (*mapper.MultipleGateways, error) {

	gateways := rpc.MemStore.GetTopKGateways("global", 10)

	var gatewayResponses []*mapper.GatewayResponse
	for _, gateway := range gateways {
		gatewayResponses = append(gatewayResponses, &mapper.GatewayResponse{
			GatewayId:     gateway.GatewayID,
			GatewayDomain: gateway.GatewayDomain,
			GatewayIp:     gateway.GatewayIP,
		})
	}

	if len(gatewayResponses) == 0 {
		return &mapper.MultipleGateways{
			Gateways: []*mapper.GatewayResponse{},
			Error: &mapper.Error{
				Code:    2,
				Message: "no gateway found",
			},
		}, nil
	}
	return &mapper.MultipleGateways{
		Gateways: gatewayResponses,
		Error:    nil,
	}, nil
}
