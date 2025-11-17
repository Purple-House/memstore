package maps

import (
	"context"

	memstore "github.com/Purple-House/memstore/registry/pkg/memstore"
	mapper "github.com/Purple-House/memstore/registry/proto"
	walpb "github.com/Purple-House/memstore/registry/wal/proto"

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

	data, err := rpc.MemStore.AddGateway(
		region,
		gatewayData,
	)

	// this should be zero lock write to WAL
	err = rpc.WALer.Append(&walpb.WalRecord{

		Op: walpb.Operation_OP_PUT_GATEWAY,
		Gateway: &walpb.GatewayPutRequest{
			GatewayDomain: gatewayData.GatewayDomain,
			GatewayIp:     gatewayData.GatewayIP,
			GatewayId:     gatewayData.GatewayID,
			Capacity: &walpb.Capacity{
				Cpu:     gatewayData.Capacity.CPU,
				Memory:  gatewayData.Capacity.Memory,
				Storage: gatewayData.Capacity.Storage,
			},
		},
	})

	if err != nil {
		return &mapper.GatewayResponse{
			Error: &mapper.Error{
				Code:    1,
				Message: err.Error(),
			},
		}, nil
	}
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
