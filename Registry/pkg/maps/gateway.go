package maps

import (
	"context"

	memstore "github.com/dipghoshraj/ingress-tunnel/registry/pkg/memstore"
	mapper "github.com/dipghoshraj/ingress-tunnel/registry/proto"
	"github.com/google/uuid"
)

func (rpc *RPCMap) RegisterGateway(ctx context.Context, req *mapper.GatewayPutRequest) (*mapper.GatewayResponse, error) {
	gatewayData := &memstore.GatewayData{
		GatewayDomain: req.GatewayDomain,
		GatewayIP:     req.GatewayIp,
		GatewayID:     uuid.New().String(),
		Capacity: memstore.Capacity{
			CPU:     req.Capacity.Cpu,
			Memory:  req.Capacity.Memory,
			Storage: req.Capacity.Storage,
		},
	}

	region := req.Region
	if region == "" {
		region = "global"
	}

	data := rpc.MemStore.AddGateway(
		region,
		gatewayData,
	)
	return &mapper.GatewayResponse{
		GatewayId:     data.GatewayID,
		GatewayDomain: data.GatewayDomain,
		GatewayIp:     data.GatewayIP,
	}, nil
}

func (rpc *RPCMap) ResolveGatewayForProxy(ctx context.Context, req *mapper.GatewayProxy) (*mapper.GatewayResponse, error) {

	gateway, exist := rpc.MemStore.GetGateway(
		req.AgentDomain,
		req.Region,
	)

	if exist {
		return &mapper.GatewayResponse{
			GatewayId:     gateway.GatewayID,
			GatewayDomain: gateway.GatewayDomain,
			GatewayIp:     gateway.GatewayIP,
		}, nil
	}
	return &mapper.GatewayResponse{
		Error: &mapper.Error{
			Code:    2,
			Message: "gateway not found",
		},
	}, nil
}
