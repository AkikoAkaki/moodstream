.PHONY: run-server run-worker up down proto fmt lint test build-server build-worker ci-local

up:
	docker-compose -f deploy/docker-compose.yaml up -d

down:
	docker-compose -f deploy/docker-compose.yaml down

run-server:
	go run cmd/server/main.go

run-worker:
	go run cmd/worker/main.go

proto:
	@echo "Generating protobuf code..."
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       api/proto/queue.proto

fmt:
	@echo "Formatting code..."
	goimports -w .

lint: fmt
	@echo "Linting code..."
	golangci-lint run

test:
	go test -v -race ./...

build-server:
	mkdir -p ./bin
	go build -v -o ./bin/server ./cmd/server

build-worker:
	mkdir -p ./bin
	go build -v -o ./bin/worker ./cmd/worker

ci-local: lint test build-server build-worker
	@echo "Local CI checks passed"
