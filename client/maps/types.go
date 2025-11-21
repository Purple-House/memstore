package maps

import pb "github.com/Purple-House/memstore/registry/client/protobufs"

type Error struct {
	Code    pb.ErrorCode
	Message string
}

type Capacity struct {
	CPU       int32
	Memory    int32
	Storage   int32
	Bandwidth int32
}

type Gateway struct {
	ID     string
	IP     string
	Domain string
	Error  *Error
}

type Agent struct {
	ID            string
	Domain        string
	GatewayID     string
	GatewayDomain string
	Capacity      Capacity
	Error         *Error
}

func gatewayFromProto(g *pb.GatewayResponse) *Gateway {
	if g == nil {
		return nil
	}
	return &Gateway{
		ID:     g.GatewayId,
		IP:     g.GatewayIp,
		Domain: g.GatewayDomain,
		Error:  errorFromProto(g.Error),
	}
}

func agentFromProto(a *pb.AgentResponse) *Agent {
	if a == nil {
		return nil
	}
	return &Agent{
		ID:            a.AgentId,
		Domain:        a.AgentDomain,
		GatewayID:     a.GatewayId,
		GatewayDomain: a.GatewayDomain,
		Capacity: Capacity{
			CPU:       a.Capacity.Cpu,
			Memory:    a.Capacity.Memory,
			Storage:   a.Capacity.Storage,
			Bandwidth: a.Capacity.Bandwidth,
		},
		Error: errorFromProto(a.Error),
	}
}

func errorFromProto(e *pb.Error) *Error {
	if e == nil {
		return nil
	}
	return &Error{
		Code:    e.Code,
		Message: e.Message,
	}
}
