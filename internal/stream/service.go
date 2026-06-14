package stream

import (
	"io"
	"log"

	pb "github.com/AkikoAkaki/moodstream/api/proto"
	"google.golang.org/grpc"
)

// Service implements pb.StreamServiceServer.
type Service struct {
	pb.UnimplementedStreamServiceServer
	batcher *Batcher
}

// NewService creates a gRPC stream service that submits events to the batcher.
func NewService(batcher *Batcher) *Service {
	return &Service{batcher: batcher}
}

// PushEvents receives a client-side stream of InteractionEvents and submits
// each one to the batcher for in-memory merging before Redis write.
func (s *Service) PushEvents(stream grpc.ClientStreamingServer[pb.InteractionEvent, pb.PushAck]) error {
	var count int64
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			log.Printf("stream-service: PushEvents session closed, received %d events", count)
			return stream.SendAndClose(&pb.PushAck{Success: true})
		}
		if err != nil {
			return err
		}
		s.batcher.Submit(event)
		count++
	}
}
