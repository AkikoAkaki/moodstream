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

	fmt.Println("=== Testing Idempotent Enqueue ===")
	fmt.Println()

	// 2. 第一次提交任务（使用幂等性 key）
	fmt.Println("1. First enqueue with idempotency_key...")
	idempotencyKey := fmt.Sprintf("order-payment-%d", time.Now().Unix())

	resp1, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:          "order-payment",
		Payload:        `{"order_id": 12345, "amount": 199.99}`,
		DelaySeconds:   10,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		log.Fatalf("First enqueue failed: %v", err)
	}
	if !resp1.Success {
		log.Fatalf("First enqueue failed: %s", resp1.ErrorMessage)
	}
	taskID1 := resp1.Id
	fmt.Printf("   ✅ Task created: %s\n", taskID1)

	// 3. 第二次提交相同的任务（相同的幂等性 key）
	time.Sleep(100 * time.Millisecond) // 稍等确保第一次写入完成

	fmt.Println("\n2. Second enqueue with same idempotency_key (should return same ID)...")
	resp2, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:          "order-payment",
		Payload:        `{"order_id": 12345, "amount": 199.99}`, // 即使 payload 不同
		DelaySeconds:   10,
		IdempotencyKey: idempotencyKey, // 相同的幂等性 key
	})
	if err != nil {
		log.Fatalf("Second enqueue failed: %v", err)
	}
	if !resp2.Success {
		log.Fatalf("Second enqueue failed: %s", resp2.ErrorMessage)
	}
	taskID2 := resp2.Id
	fmt.Printf("   ✅ Task ID: %s\n", taskID2)

	// 4. 验证两次返回的 ID 是否相同
	fmt.Println("\n3. Verifying idempotency...")
	if taskID1 == taskID2 {
		fmt.Printf("   ✅ SUCCESS: Both requests returned the same task ID: %s\n", taskID1)
		fmt.Println("   👉 This proves idempotency is working correctly!")
	} else {
		fmt.Printf("   ❌ FAILED: Different task IDs:\n")
		fmt.Printf("      First:  %s\n", taskID1)
		fmt.Printf("      Second: %s\n", taskID2)
		log.Fatal("Idempotency test failed!")
	}

	// 5. 测试不使用幂等性 key 的情况（应该创建新任务）
	fmt.Println("\n4. Testing without idempotency_key (should create new task)...")
	resp3, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "order-payment",
		Payload:      `{"order_id": 67890, "amount": 299.99}`,
		DelaySeconds: 10,
		// 不提供 IdempotencyKey
	})
	if err != nil {
		log.Fatalf("Third enqueue failed: %v", err)
	}
	taskID3 := resp3.Id
	fmt.Printf("   ✅ New task created: %s\n", taskID3)

	if taskID3 != taskID1 {
		fmt.Println("   ✅ Correct: Different task ID when no idempotency_key is provided")
	} else {
		log.Fatal("   ❌ FAILED: Should have created a new task!")
	}

	// 6. 测试不同的幂等性 key（应该创建新任务）
	fmt.Println("\n5. Testing with different idempotency_key (should create new task)...")
	differentKey := fmt.Sprintf("order-refund-%d", time.Now().Unix())
	resp4, err := client.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:          "order-payment",
		Payload:        `{"order_id": 99999, "amount": 399.99}`,
		DelaySeconds:   10,
		IdempotencyKey: differentKey,
	})
	if err != nil {
		log.Fatalf("Fourth enqueue failed: %v", err)
	}
	taskID4 := resp4.Id
	fmt.Printf("   ✅ New task created: %s\n", taskID4)

	if taskID4 != taskID1 {
		fmt.Println("   ✅ Correct: Different task ID for different idempotency_key")
	} else {
		log.Fatal("   ❌ FAILED: Should have created a new task!")
	}

	fmt.Println("\n🎉 All idempotency tests passed!")
	fmt.Println("\n📊 Summary:")
	fmt.Printf("   - Same idempotency_key: %s (returned twice)\n", taskID1)
	fmt.Printf("   - No idempotency_key:   %s (new task)\n", taskID3)
	fmt.Printf("   - Different key:        %s (new task)\n", taskID4)
}
