package maps

import (
	"context"
	"errors"

	"github.com/Purple-House/memstore/registry/client/internal"
	pb "github.com/Purple-House/memstore/registry/client/protobufs"
)

type Client struct {
	grpc *internal.Client
}

func New(opts ...Option) (*Client, error) {
	cfg := defaultOptions()
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.Address == "" {
		return nil, errors.New("address must be set")
	}

	cli, err := internal.New(internal.Config{
		Address: cfg.Address,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}

	return &Client{grpc: cli}, nil
}

func (c *Client) Close() error {
	return c.grpc.Close()
}

//
// Public API
//

func (c *Client) RegisterGateway(ctx context.Context, req *pb.GatewayPutRequest) (*Gateway, error) {
	res, err := c.grpc.RegisterGateway(ctx, req)
	if err != nil {
		return nil, err
	}
	return gatewayFromProto(res), nil
}

func (c *Client) RegisterAgent(ctx context.Context, req *pb.AgentConnectionRequest) (*Agent, error) {
	res, err := c.grpc.RegisterAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentFromProto(res), nil
}

func (c *Client) ResolveGatewayForAgent(ctx context.Context, region string) ([]Gateway, error) {
	res, err := c.grpc.ResolveGatewayForAgent(ctx, &pb.GatewayHandshake{Region: region})
	if err != nil {
		return nil, err
	}

	var out []Gateway
	for _, g := range res.Gateways {
		out = append(out, *gatewayFromProto(g))
	}
	return out, nil
}

func (c *Client) ResolveGatewayForProxy(ctx context.Context, req *pb.GatewayProxy) (*Gateway, error) {
	res, err := c.grpc.ResolveGatewayForProxy(ctx, req)
	if err != nil {
		return nil, err
	}
	return gatewayFromProto(res), nil
}
