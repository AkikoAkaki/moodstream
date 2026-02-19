package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	var (
		configFile = flag.String("config", "", "Path to config file, e.g. ./config/config.yaml")
		configDir  = flag.String("config-dir", "", "Directory containing config.yaml")
		serverAddr = flag.String("server-addr", "", "gRPC server address, e.g. localhost:9090")
		tick       = flag.Duration("poll-interval", time.Second, "Polling interval for Retrieve requests")
	)
	flag.Parse()

	cfg, err := conf.LoadWithOptions(conf.LoadOptions{
		ConfigFile: *configFile,
		ConfigDir:  *configDir,
	})
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	grpcAddr := *serverAddr
	if grpcAddr == "" {
		grpcAddr = fmt.Sprintf("localhost:%d", cfg.Server.GrpcPort)
	}

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to server: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("failed to close grpc connection: %v", closeErr)
		}
	}()

	client := pb.NewDelayQueueServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Worker started, polling %s", grpcAddr)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*tick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				resp, err := client.Retrieve(ctx, &pb.RetrieveRequest{
					Topic:     "default",
					BatchSize: 10,
				})
				if err != nil {
					log.Printf("retrieve failed: %v", err)
					continue
				}

				for _, task := range resp.Tasks {
					log.Printf("[EXECUTE] task=%s payload=%s", task.Id, task.Payload)

					if _, ackErr := client.Ack(ctx, &pb.AckRequest{Id: task.Id}); ackErr != nil {
						log.Printf("[ERROR] ack failed task=%s err=%v", task.Id, ackErr)
						continue
					}
					log.Printf("[ACK] task=%s", task.Id)
				}
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Worker shutting down...")
	cancel()
	wg.Wait()
	log.Println("Worker stopped")
}
