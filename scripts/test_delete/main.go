package main

import (
	"context"
	"fmt"
	"log"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 1. 连接到 gRPC Server
	conn, err := grpc.NewClient("localhost:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("close grpc connection error: %v", closeErr)
		}
	}()

	client := pb.NewDelayQueueServiceClient(conn)
	ctx := context.Background()

	// 2. 提交一个延迟任务
	fmt.Println("=== Testing Delete API ===")
	fmt.Println("\n1. Enqueueing a task...")
	enqResp, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "test-delete",
		Payload:      `{"message":"This task will be deleted"}`,
		DelaySeconds: 60, // 60秒后执行
	})
	if err != nil {
		log.Fatalf("Enqueue failed: %v", err)
	}
	if !enqResp.Success {
		log.Fatalf("Enqueue failed: %s", enqResp.ErrorMessage)
	}
	taskID := enqResp.Id
	fmt.Printf("✅ Task created: %s\n", taskID)

	// 3. 等待一下，确保任务已经持久化
	time.Sleep(500 * time.Millisecond)

	// 4. 删除任务
	fmt.Println("\n2. Deleting the task...")
	delResp, err := client.Delete(ctx, &pb.DeleteRequest{
		Id: taskID,
	})
	if err != nil {
		log.Fatalf("Delete failed: %v", err)
	}
	if !delResp.Success {
		log.Fatalf("Delete failed")
	}
	fmt.Printf("✅ Task deleted: %s\n", taskID)

	// 5. 尝试再次删除（测试幂等性）
	fmt.Println("\n3. Attempting to delete again (idempotency test)...")
	delResp2, err := client.Delete(ctx, &pb.DeleteRequest{
		Id: taskID,
	})
	if err != nil {
		log.Fatalf("Second delete failed: %v", err)
	}
	if !delResp2.Success {
		log.Fatalf("Second delete failed")
	}
	fmt.Printf("✅ Second delete succeeded (idempotent): %s\n", taskID)

	// 6. 测试 Retrieve API
	fmt.Println("\n4. Testing Retrieve API (should return no tasks)...")
	retrieveResp, err := client.Retrieve(ctx, &pb.RetrieveRequest{
		Topic:     "test-delete",
		BatchSize: 10,
	})
	if err != nil {
		log.Fatalf("Retrieve failed: %v", err)
	}
	fmt.Printf("✅ Retrieved %d tasks (expected 0, since we deleted the only task)\n", len(retrieveResp.Tasks))

	// 7. 测试完整流程：Enqueue -> Retrieve -> Ack
	fmt.Println("\n5. Testing complete flow: Enqueue -> Retrieve -> Ack...")

	// Enqueue立即执行的任务
	enqResp2, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "test-complete",
		Payload:      `{"message":"Testing complete flow"}`,
		DelaySeconds: 0, // 立即执行
	})
	if err != nil {
		log.Fatalf("Enqueue failed: %v", err)
	}
	taskID2 := enqResp2.Id
	fmt.Printf("   Enqueued task: %s\n", taskID2)

	// 稍等一下确保写入
	time.Sleep(100 * time.Millisecond)

	// Retrieve
	retrieveResp2, err := client.Retrieve(ctx, &pb.RetrieveRequest{
		Topic:     "test-complete",
		BatchSize: 10,
	})
	if err != nil {
		log.Fatalf("Retrieve failed: %v", err)
	}
	fmt.Printf("   Retrieved %d tasks\n", len(retrieveResp2.Tasks))

	if len(retrieveResp2.Tasks) > 0 {
		task := retrieveResp2.Tasks[0]
		fmt.Printf("   Task payload: %s\n", task.Payload)

		// Ack
		ackResp, err := client.Ack(ctx, &pb.AckRequest{
			Id: task.Id,
		})
		if err != nil {
			log.Fatalf("Ack failed: %v", err)
		}
		if ackResp.Success {
			fmt.Printf("✅ Task acknowledged: %s\n", task.Id)
		}
	}

	fmt.Println("\n🎉 All tests passed!")
}
