package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/storage/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	cfg, err := conf.LoadWithOptions(conf.LoadOptions{})
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	log.Printf("starting %s [%s] version=%s", cfg.App.Name, cfg.App.Env, Version)

	store := redis.NewStore(cfg.Redis.Addr)
	defer store.Close()

	// TODO Phase 3: wire SSEBroadcaster, Aggregator, gRPC StreamService

	grpcAddr := fmt.Sprintf(":%d", cfg.Server.GrpcPort)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}

	grpcSrv := grpc.NewServer()
	reflection.Register(grpcSrv)
	// TODO Phase 3: pb.RegisterStreamServiceServer(grpcSrv, streamSvc)

	go func() {
		log.Printf("gRPC server listening at %v", lis.Addr())
		if err := grpcSrv.Serve(lis); err != nil {
			log.Fatalf("gRPC serve error: %v", err)
		}
	}()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	// TODO Phase 3: POST /events/push, GET /stream/results

	httpAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpSrv := &http.Server{Addr: httpAddr, Handler: httpMux}
	go func() {
		log.Printf("HTTP server listening at %s", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit

	log.Println("shutting down...")
	grpcSrv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("server stopped")
}
