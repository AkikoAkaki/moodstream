package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/observability"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

type workerOptions struct {
	configFile     string
	configDir      string
	serverAddr     string
	pollInterval   time.Duration
	rpcTimeout     time.Duration
	topic          string
	batchSize      int32
	processorCount int
	queueCapacity  int
	ackRetry       int
}

func main() {
	showVer := flag.Bool("version", false, "Print version information and exit")
	opts := parseFlags()
	if *showVer {
		fmt.Printf("worker version=%s build_time=%s\n", Version, BuildTime)
		return
	}

	cfg, err := conf.LoadWithOptions(conf.LoadOptions{
		ConfigFile: opts.configFile,
		ConfigDir:  opts.configDir,
	})
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	metricsSrv := startMetricsServer(":8082")

	grpcAddr := opts.serverAddr
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
	taskCh := make(chan *pb.Task, opts.queueCapacity)
	stopFetcher := make(chan struct{})

	var fetchWG sync.WaitGroup
	fetchWG.Add(1)
	go func() {
		defer fetchWG.Done()
		runFetcher(client, opts, taskCh, stopFetcher)
	}()

	var procWG sync.WaitGroup
	for i := 0; i < opts.processorCount; i++ {
		procWG.Add(1)
		go func(workerID int) {
			defer procWG.Done()
			runProcessor(workerID, client, opts, taskCh)
		}(i + 1)
	}

	log.Printf(
		"Worker started: server=%s processors=%d queue_capacity=%d batch_size=%d topic=%s",
		grpcAddr,
		opts.processorCount,
		opts.queueCapacity,
		opts.batchSize,
		opts.topic,
	)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	<-quit
	log.Println("Shutdown signal received, stopping fetcher...")
	close(stopFetcher)

	// 1) Stop pull loop first.
	fetchWG.Wait()
	// 2) Then let processors drain queued/in-flight tasks.
	close(taskCh)
	// 3) Exit only after all Ack/Nack paths are finished.
	procWG.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(ctx); err != nil {
		log.Printf("metrics server shutdown error: %v", err)
	}

	log.Println("Worker stopped gracefully")
}

func parseFlags() workerOptions {
	var opts workerOptions
	batchSize := 10

	flag.StringVar(&opts.configFile, "config", "", "Path to config file, e.g. ./config/config.yaml")
	flag.StringVar(&opts.configDir, "config-dir", "", "Directory containing config.yaml")
	flag.StringVar(&opts.serverAddr, "server-addr", "", "gRPC server address, e.g. localhost:9090")
	flag.DurationVar(&opts.pollInterval, "poll-interval", time.Second, "Fetcher poll interval for Retrieve")
	flag.DurationVar(&opts.rpcTimeout, "rpc-timeout", 5*time.Second, "Timeout for each Retrieve/Ack/Nack RPC")
	flag.StringVar(&opts.topic, "topic", "default", "Retrieve topic")
	flag.IntVar(&batchSize, "batch-size", 10, "Retrieve batch size per fetch")
	flag.IntVar(&opts.processorCount, "processors", 4, "Number of concurrent processors")
	flag.IntVar(&opts.queueCapacity, "queue-capacity", 128, "Buffered task queue capacity")
	flag.IntVar(&opts.ackRetry, "ack-retry", 3, "Max retries for Ack/Nack RPC")
	flag.Parse()

	opts.batchSize = int32(batchSize)
	if opts.batchSize <= 0 {
		opts.batchSize = 10
	}
	if opts.pollInterval <= 0 {
		opts.pollInterval = time.Second
	}
	if opts.rpcTimeout <= 0 {
		opts.rpcTimeout = 5 * time.Second
	}
	if opts.processorCount <= 0 {
		opts.processorCount = 1
	}
	if opts.queueCapacity <= 0 {
		opts.queueCapacity = opts.processorCount
	}
	if opts.ackRetry <= 0 {
		opts.ackRetry = 1
	}

	return opts
}

func runFetcher(client pb.DelayQueueServiceClient, opts workerOptions, taskCh chan<- *pb.Task, stop <-chan struct{}) {
	ticker := time.NewTicker(opts.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			log.Println("Fetcher stopped")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), opts.rpcTimeout)
		resp, err := client.Retrieve(ctx, &pb.RetrieveRequest{
			Topic:     opts.topic,
			BatchSize: opts.batchSize,
		})
		cancel()
		if err != nil {
			log.Printf("fetcher retrieve failed: %v", err)
		} else {
			for _, task := range resp.Tasks {
				// Bounded channel provides backpressure and capacity control.
				taskCh <- task
			}
		}

		select {
		case <-stop:
			log.Println("Fetcher stopped")
			return
		case <-ticker.C:
		}
	}
}

func runProcessor(workerID int, client pb.DelayQueueServiceClient, opts workerOptions, taskCh <-chan *pb.Task) {
	for task := range taskCh {
		start := time.Now()
		topic := task.Topic
		if topic == "" {
			topic = opts.topic
		}

		log.Printf("[PROCESSOR-%d] execute task=%s topic=%s", workerID, task.Id, task.Topic)

		err := executeTask(task)
		if err != nil {
			if nackErr := nackWithRetry(client, task, opts.rpcTimeout, opts.ackRetry); nackErr != nil {
				log.Printf("[PROCESSOR-%d] nack failed task=%s err=%v", workerID, task.Id, nackErr)
			} else {
				log.Printf("[PROCESSOR-%d] nack task=%s", workerID, task.Id)
			}
			observability.ObserveTaskProcessDuration(topic, time.Since(start))
			continue
		}

		if ackErr := ackWithRetry(client, task.Id, opts.rpcTimeout, opts.ackRetry); ackErr != nil {
			log.Printf("[PROCESSOR-%d] ack failed task=%s err=%v", workerID, task.Id, ackErr)
			observability.ObserveTaskProcessDuration(topic, time.Since(start))
			continue
		}
		log.Printf("[PROCESSOR-%d] ack task=%s", workerID, task.Id)
		observability.ObserveTaskProcessDuration(topic, time.Since(start))
	}

	log.Printf("[PROCESSOR-%d] stopped", workerID)
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

func executeTask(task *pb.Task) error {
	// Placeholder for business logic execution.
	log.Printf("[EXECUTE] task=%s payload=%s", task.Id, task.Payload)
	return nil
}

func ackWithRetry(client pb.DelayQueueServiceClient, id string, timeout time.Duration, maxRetry int) error {
	var lastErr error
	for i := 0; i < maxRetry; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err := client.Ack(ctx, &pb.AckRequest{Id: id})
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("ack task %s failed after %d retries: %w", id, maxRetry, lastErr)
}

func nackWithRetry(client pb.DelayQueueServiceClient, task *pb.Task, timeout time.Duration, maxRetry int) error {
	if task == nil {
		return errors.New("nil task")
	}

	req := &pb.NackRequest{
		Id:          task.Id,
		Topic:       task.Topic,
		Payload:     task.Payload,
		ExecuteTime: task.ExecuteTime,
		RetryCount:  task.RetryCount,
		MaxRetries:  task.MaxRetries,
		CreatedAt:   task.CreatedAt,
	}

	var lastErr error
	for i := 0; i < maxRetry; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err := client.Nack(ctx, req)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("nack task %s failed after %d retries: %w", task.Id, maxRetry, lastErr)
}
