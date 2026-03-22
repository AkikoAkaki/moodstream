package handler

import (
	"encoding/json"
	"net/http"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/stream"
)

// PushHandler returns an http.HandlerFunc that accepts a JSON-encoded
// InteractionEvent via POST and submits it to the batcher.
// This is the REST alternative to the gRPC PushEvents stream.
func PushHandler(batcher *stream.Batcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var event pb.InteractionEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if event.VideoId == "" || event.RawText == "" {
			http.Error(w, "video_id and raw_text are required", http.StatusBadRequest)
			return
		}

		batcher.Submit(&event)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			return // client gone
		}
	}
}
