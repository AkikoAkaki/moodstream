package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/AkikoAkaki/async-task-platform/internal/stream"
)

// SSEHandler returns an http.HandlerFunc that streams WindowResult events
// to the client using Server-Sent Events.
func SSEHandler(broadcaster *stream.Broadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := broadcaster.Subscribe()
		defer broadcaster.Unsubscribe(ch)

		// Send initial comment so client knows connection is alive.
		if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
			return
		}
		flusher.Flush()

		for {
			select {
			case result, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(result)
				if err != nil {
					log.Printf("sse: marshal error: %v", err)
					continue
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					return // client disconnected
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
