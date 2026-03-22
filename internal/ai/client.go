package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
)

// Client calls an OpenAI-compatible chat completion API (e.g. Qwen).
type Client struct {
	httpClient *http.Client
	baseURL    string // e.g. "https://dashscope.aliyuncs.com/compatible-mode/v1"
	apiKey     string
	model      string
}

// Result is the structured output from the LLM for one aggregation window.
type Result struct {
	EmotionTag string `json:"emotion_tag"`
	CoreTopic  string `json:"core_topic"`
}

// New creates an AI client. baseURL should NOT include a trailing slash.
func New(baseURL, apiKey, model string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
	}
}

const systemPrompt = `You are a real-time audience sentiment analyzer for a live video stream.
You will receive a batch of viewer comments (danmu/弹幕). Some may include a repeat count indicating how many viewers posted that exact text.

Return a JSON object with exactly two fields:
- "emotion_tag": a single English word capturing the dominant audience emotion (e.g. "excited", "confused", "amused", "bored", "angry", "wholesome")
- "core_topic": one concise sentence (≤20 words) summarizing what the audience is reacting to

Return ONLY valid JSON, no markdown fences, no extra text.`

// Analyze sends a batch of events to the LLM and returns the analysis.
func (c *Client) Analyze(ctx context.Context, events []*pb.InteractionEvent) (*Result, error) {
	prompt := buildUserPrompt(events)

	body := chatRequest{
		Model: c.model,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call LLM API: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("ai: close response body: %v", cerr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned %d: %s", resp.StatusCode, truncate(respBody, 200))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result Result
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse LLM JSON %q: %w", truncate([]byte(content), 120), err)
	}
	return &result, nil
}

func buildUserPrompt(events []*pb.InteractionEvent) string {
	var sb strings.Builder
	sb.WriteString("Viewer comments:\n")
	for _, e := range events {
		count := e.RepeatCount
		if count <= 1 {
			fmt.Fprintf(&sb, "- %s\n", e.RawText)
		} else {
			fmt.Fprintf(&sb, "- %s (×%d)\n", e.RawText, count)
		}
	}
	return sb.String()
}

// --- OpenAI-compatible request/response types ---

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
}

type choice struct {
	Message message `json:"message"`
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
