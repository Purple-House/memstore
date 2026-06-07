package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"runtime/debug"

	"github.com/gorilla/mux"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	mapper "github.com/odio4u/agni-schema/maps"
	walpb "github.com/odio4u/agni-schema/wal"
	"github.com/odio4u/mem-sdk/certengine/pkg"
	"github.com/odio4u/memstore/seeder/pkg/api"
	"github.com/odio4u/memstore/seeder/pkg/maps"
	memstore "github.com/odio4u/memstore/seeder/pkg/memstore"
	wal "github.com/odio4u/memstore/seeder/wal"
	"gopkg.in/yaml.v3"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

type Seeder struct {
	IP     string `yaml:"ip"`
	Port   string `yaml:"port"`
	Dns    string `yaml:"dns"`
	Name   string `yaml:"name"`
	Viewer string `yaml:"viewer"`
	Region string `yaml:"region"`
}

type Config struct {
	Version string `yaml:"version"`
	Seeder  Seeder `yaml:"Seeder"`
}

func gracefulShutdown(server *grpc.Server) {

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")

	// Attempt graceful shutdown
	server.Stop()

}

func certFingurePrint() (*string, error) {
	permfile := "server.pem" // replace with your file path
	certPEM, err := os.ReadFile(permfile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file use `seeder -gen-cert` to create certificates")
	}

	block, _ := pem.Decode(certPEM)

	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	log.Printf("Client CERT fingerprint (SHA256): %s", fingerprint)
	return &fingerprint, nil
}

func generateCerts(config Config) error {

	routerIps := []string{config.Seeder.IP}
	dns := []string{config.Seeder.Dns}

	_, err := pkg.GenerateSelfSignedGPR(config.Seeder.Name, routerIps, dns)
	if err != nil {
		return err
	}

	log.Println("Certificates generated successfully.")
	return nil
}

func main() {

	data, err := os.ReadFile("seeder-config.yaml")
	if err != nil {
		log.Fatal("[Agni Seeder] Can not read the seeder config file")
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Fatal("[Agni Seeder] Falied to unmarsal seeder config")
	}

	genCert := flag.Bool("gen-cert", false, "Generate self-signed certificates")
	flag.Parse()

	if *genCert {
		if err := generateCerts(config); err != nil {
			log.Fatalf("[Agni Seeder] Failed to generate certs: %v", err)
		}
		return // exit after generating certs
	}

	log.Println("Registry Service for Ingress Tunnel")

	cert, err := tls.LoadX509KeyPair("server.pem", "server-key.pem")
	if err != nil {
		log.Fatalf("[Agni Seeder] failed to load server certificate use `seeder -gen-cert` to create certificates")
	}

	servertLs := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,

		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		},

		SessionTicketsDisabled:   true,
		PreferServerCipherSuites: true,

		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
		Renegotiation: tls.RenegotiateNever,
	}

	fingureprint, err := certFingurePrint()
	if err != nil {
		log.Fatalf("[Agni Seeder] Failed to print certificate fingerprint: %v", err)
	}

	port := config.Seeder.Port
	if port == "" {
		port = "50051"
	}
	port = fmt.Sprintf(":%s", port)

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("[Agni Seeder] failed to listen: %v", err)
	}

	recoveryOpts := []grpc_recovery.Option{
		grpc_recovery.WithRecoveryHandler(func(p interface{}) error {
			stack := string(debug.Stack())
			log.Printf("[PANIC RECOVERED] %v\nSTACK TRACE:\n%s", p, stack)
			return fmt.Errorf("internal server error")
		}),
	}

	waler, err := wal.OpenWAL()
	if err != nil {
		log.Fatalf("[Agni Seeder] failed to open WAL: %v", err)
	}
	defer waler.Close()

	s := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(servertLs)),
		grpc.UnaryInterceptor(grpc_recovery.UnaryServerInterceptor(recoveryOpts...)),
		grpc.StreamInterceptor(grpc_recovery.StreamServerInterceptor(recoveryOpts...)),
	)

	store := memstore.NewMemStore()
	mapper.RegisterMapsServer(s, &maps.RPCMap{
		MemStore: store,
		WALer:    waler,
	})
	reflection.Register(s)

	_ = waler.Replay(func(wr *walpb.WalRecord) error {
		return wal.ApplyRecord(store, wr)
	})

	expiryCtx, cancelExpiry := context.WithCancel(context.Background())
	defer cancelExpiry()
	store.SetTTL(48 * time.Hour)
	store.StartExpiryWorker(expiryCtx, waler)

	apis := api.NewApi(store)
	router := mux.NewRouter()

	api.SetRoutes(router, apis)

	readyGRPC := make(chan struct{})
	readyHTTP := make(chan struct{})

	// Start the server
	go func() {
		log.Println("GRPC server listening in :", port)
		close(readyGRPC)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("[Agni Seeder] failed to serve: %v", err)
		}
	}()

	httpserver := &http.Server{
		Addr:         ":" + config.Seeder.Viewer,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting server on port %s", config.Seeder.Viewer)
		close(readyHTTP)
		if err := httpserver.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for both servers to signal readiness
	<-readyGRPC
	<-readyHTTP

	// Now safe to run your data-saving function
	log.Println("Both servers are up. Saving data...")
	store.AddSeeder(
		&memstore.SeederData{
			SeederID:       "register-q",
			Name:           config.Seeder.Name,
			Dns:            config.Seeder.Dns,
			SeedIP:         config.Seeder.IP,
			SeedPort:       config.Seeder.Port,
			Region:         config.Seeder.Region,
			VerifiableHash: *fingureprint,
		},
	)

	gracefulShutdown(s)

}
