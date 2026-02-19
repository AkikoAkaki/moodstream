package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/observability"
	"github.com/AkikoAkaki/async-task-platform/internal/queue"
	"github.com/AkikoAkaki/async-task-platform/internal/scheduler"
	"github.com/AkikoAkaki/async-task-platform/internal/storage/redis"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	var (
		configFile = flag.String("config", "", "Path to config file, e.g. ./config/config.yaml")
		configDir  = flag.String("config-dir", "", "Directory containing config.yaml")
		grpcPort   = flag.Int("grpc-port", 0, "Override gRPC port")
		redisAddr  = flag.String("redis-addr", "", "Override Redis address")
	)
	flag.Parse()

	cfg, err := conf.LoadWithOptions(conf.LoadOptions{
		ConfigFile: *configFile,
		ConfigDir:  *configDir,
	})
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Flag overrides have highest priority over file/env defaults.
	if *grpcPort > 0 {
		cfg.Server.GrpcPort = *grpcPort
	}
	if *redisAddr != "" {
		cfg.Redis.Addr = *redisAddr
	}

	log.Printf("Starting %s [%s]...", cfg.App.Name, cfg.App.Env)
	metricsSrv := startMetricsServer(":8081")

	store := redis.NewStore(cfg.Redis.Addr)
	wd := scheduler.NewWatchdog(cfg.Queue, store)
	wd.Start()

	addr := fmt.Sprintf(":%d", cfg.Server.GrpcPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	svc := queue.NewService(store)
	pb.RegisterDelayQueueServiceServer(s, svc)
	reflection.Register(s)

	go func() {
		log.Printf("gRPC server listening at %v", lis.Addr())
		if serveErr := s.Serve(lis); serveErr != nil {
			log.Fatalf("failed to serve: %v", serveErr)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit

	log.Println("Shutting down gRPC server...")
	wd.Stop()
	s.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(ctx); err != nil {
		log.Printf("metrics server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

func startMetricsServer(addr string) *http.Server {
	observability.Register()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("metrics server listening at %s/metrics", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server error: %v", err)
		}
	}()

	return srv
}
