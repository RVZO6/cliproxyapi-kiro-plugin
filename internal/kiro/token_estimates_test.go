package kiro

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestTokenEstimateDoesNotCountBase64AsText(t *testing.T) {
	for _, nested := range []bool{false, true} {
		var estimates []int
		for _, size := range []int{100, 1000000} {
			image := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("A", size)}}
			var block any = image
			if nested {
				block = map[string]any{"type": "tool_result", "tool_use_id": "screenshot", "content": []any{map[string]any{"type": "text", "text": "image follows"}, image}}
			}
			payload, _ := json.Marshal(map[string]any{"model": "claude-opus-5-5", "messages": []any{map[string]any{"role": "user", "content": []any{block}}}})
			original := string(payload)
			estimates = append(estimates, estimateRequestTokens(payload))
			if string(payload) != original {
				t.Fatal("estimator mutated request")
			}
		}
		if estimates[0] != estimates[1] || estimates[1] < 1600 || estimates[1] > 2000 {
			t.Fatalf("base64 size inflated estimate (nested=%v): %v", nested, estimates)
		}
	}
}

func TestTokenEstimatesIncludeTextAndToolArguments(t *testing.T) {
	small := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	large := []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("text", 1000) + `"}]}`)
	if estimateRequestTokens(large) <= estimateRequestTokens(small) {
		t.Fatal("text ignored")
	}
	if estimateRequestTokens([]byte("bad JSON")) != estimateTokens(8) {
		t.Fatal("invalid JSON fallback changed")
	}
	calls := []toolCall{{id: "id", name: "write_file", input: json.RawMessage(`{"text":"` + strings.Repeat("text", 1000) + `"}`)}}
	usage := aggregateToClaudeMessage("", calls, "model", small).Usage
	if usage.InputTokens != estimateRequestTokens(small) || usage.OutputTokens != estimateOutputTokens("", calls) || usage.OutputTokens < 1000 {
		t.Fatalf("tool-only response undercounted: %+v", usage)
	}
	events := logicalStreamEvents(t, buildClaudeStreamChunks("", calls, "model", usage.InputTokens))
	for _, ev := range events {
		if sseEventType(ev.Payload) == "message_delta" {
			var delta struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(sseData(ev.Payload)), &delta); err != nil {
				t.Fatal(err)
			}
			if delta.Usage.OutputTokens != usage.OutputTokens {
				t.Fatal("stream/non-stream token estimate differs")
			}
		}
	}
}
