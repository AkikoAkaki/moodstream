# Stage 1: Builder
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /bin/server ./cmd/server

# Stage 2: Runner
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/server /app/server
COPY --from=builder /app/config/config.example.yaml /app/config.yaml

EXPOSE 8080 9090

CMD ["./server"]
