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

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/ai"
	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/handler"
	redisstore "github.com/AkikoAkaki/async-task-platform/internal/storage/redis"
	"github.com/AkikoAkaki/async-task-platform/internal/stream"
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

	// --- Storage ---
	store := redisstore.NewStoreWith(redisstore.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("store close error: %v", err)
		}
	}()

	// --- Core components ---
	broadcaster := stream.NewBroadcaster()

	batcher := stream.NewBatcher(store, 200*time.Millisecond)
	batcher.Start()
	defer batcher.Stop()

	aiClient := ai.New(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.Model)

	aggregator := stream.NewAggregator(stream.AggregatorConfig{
		Store:        store,
		AI:           aiClient,
		Broadcaster:  broadcaster,
		Batcher:      batcher,
		WindowSize:   time.Duration(cfg.Stream.WindowSizeSeconds) * time.Second,
		MaxBatchSize: cfg.Stream.MaxBatchSize,
	})
	aggregator.Start()
	defer aggregator.Stop()

	streamSvc := stream.NewService(batcher)

	// --- gRPC server ---
	grpcAddr := fmt.Sprintf(":%d", cfg.Server.GrpcPort)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}

	grpcSrv := grpc.NewServer()
	reflection.Register(grpcSrv)
	pb.RegisterStreamServiceServer(grpcSrv, streamSvc)

	go func() {
		log.Printf("gRPC server listening at %v", lis.Addr())
		if err := grpcSrv.Serve(lis); err != nil {
			log.Fatalf("gRPC serve error: %v", err)
		}
	}()

	// --- HTTP server ---
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintln(w, "ok"); err != nil {
			log.Printf("healthz write error: %v", err)
		}
	})
	httpMux.HandleFunc("/events/push", handler.PushHandler(batcher))
	httpMux.HandleFunc("/stream/results", handler.SSEHandler(broadcaster))

	httpAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpSrv := &http.Server{Addr: httpAddr, Handler: httpMux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("HTTP server listening at %s", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// --- Graceful shutdown ---
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
