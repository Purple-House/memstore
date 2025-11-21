package internal

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/Purple-House/memstore/registry/client/protobufs"
)

type Config struct {
	Address string
	APIKey  string
	Timeout time.Duration
}

type Client struct {
	conn    *grpc.ClientConn
	rpc     pb.MapsClient
	apiKey  string
	timeout time.Duration
}

func New(cfg Config) (*Client, error) {
	conn, err := grpc.Dial(
		cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:    conn,
		rpc:     pb.NewMapsClient(conn),
		apiKey:  cfg.APIKey,
		timeout: cfg.Timeout,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, c.timeout)
		ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", c.apiKey)
		return ctx, cancel
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", c.apiKey)
	return ctx, func() {}
}

//
// gRPC call wrappers
//

func (c *Client) RegisterGateway(ctx context.Context, req *pb.GatewayPutRequest) (*pb.GatewayResponse, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()
	return c.rpc.RegisterGateway(ctx, req)
}

func (c *Client) RegisterAgent(ctx context.Context, req *pb.AgentConnectionRequest) (*pb.AgentResponse, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()
	return c.rpc.RegisterAgent(ctx, req)
}

func (c *Client) ResolveGatewayForAgent(ctx context.Context, req *pb.GatewayHandshake) (*pb.MultipleGateways, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()
	return c.rpc.ResolveGatewayForAgent(ctx, req)
}

func (c *Client) ResolveGatewayForProxy(ctx context.Context, req *pb.GatewayProxy) (*pb.GatewayResponse, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()
	return c.rpc.ResolveGatewayForProxy(ctx, req)
}
