.PHONY: up down run-server proto fmt lint test build-server ci-local

up:
	docker-compose -f deploy/docker-compose.yaml up -d

down:
	docker-compose -f deploy/docker-compose.yaml down

run-server:
	go run cmd/server/main.go

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       api/proto/stream.proto

fmt:
	goimports -w .

lint: fmt
	golangci-lint run

test:
	go test -v -race ./...

build-server:
	mkdir -p ./bin
	go build -v -o ./bin/server ./cmd/server

ci-local: lint test build-server
	@echo "Local CI checks passed"
