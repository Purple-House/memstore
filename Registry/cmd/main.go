package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"runtime/debug"

	"github.com/Purple-House/memstore/registry/pkg/maps"
	memstore "github.com/Purple-House/memstore/registry/pkg/memstore"
	mapper "github.com/Purple-House/memstore/registry/proto"
	wal "github.com/Purple-House/memstore/registry/wal"
	walpb "github.com/Purple-House/memstore/registry/wal/proto"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func gracefulShutdown(server *grpc.Server) {

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")

	// Attempt graceful shutdown
	server.Stop()

}

func main() {
	fmt.Println("Registry Service for Ingress Tunnel")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	recoveryOpts := []grpc_recovery.Option{
		grpc_recovery.WithRecoveryHandler(func(p interface{}) error {
			stack := string(debug.Stack())
			log.Printf("[PANIC RECOVERED] %v\nSTACK TRACE:\n%s", p, stack)
			return fmt.Errorf("internal server error")
		}),
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_recovery.UnaryServerInterceptor(recoveryOpts...)),
		grpc.StreamInterceptor(grpc_recovery.StreamServerInterceptor(recoveryOpts...)),
	)

	store := memstore.NewMemStore()
	mapper.RegisterMapsServer(s, &maps.RPCMap{
		MemStore: store,
	})
	reflection.Register(s)

	waler, err := wal.OpenWAl()
	if err != nil {
		log.Fatalf("failed to open WAL: %v", err)
	}
	defer waler.Close()
	_ = waler.Replay(func(wr *walpb.WalRecord) error {
		return wal.ApplyRecord(store, wr)

	})

	// Start the server
	fmt.Println("Server is running on port 50051")
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	gracefulShutdown(s)

}
