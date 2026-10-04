package kiro

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"

	"github.com/xiaokui-dev/cliproxyapi-kiro-plugin/internal/hostapi"
)

func TestFetchKiroEventsRetriesEmptyResponse(t *testing.T) {
	payload, err := json.Marshal(claudeRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []claudeMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err != nil {
		t.Fatalf("marshal Claude request: %v", err)
	}
	storage, err := json.Marshal(kiroCredential{AccessToken: "test-access-token"})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	rawRequest, err := json.Marshal(executorRequest{ExecutorRequest: pluginapi.ExecutorRequest{
		Model:       "claude-sonnet-4-5",
		Payload:     payload,
		StorageJSON: storage,
	}})
	if err != nil {
		t.Fatalf("marshal executor request: %v", err)
	}

	oldHTTPDo := kiroHTTPDo
	t.Cleanup(func() { kiroHTTPDo = oldHTTPDo })
	calls := 0
	kiroHTTPDo = func(req hostapi.HTTPRequest) (*hostapi.HTTPResponse, error) {
		calls++
		switch calls {
		case 1, 2:
			return &hostapi.HTTPResponse{StatusCode: 200}, nil
		case 3:
			body := encodeFrame("assistantResponseEvent", `{"content":"recovered"}`)
			return &hostapi.HTTPResponse{StatusCode: 200, Body: body}, nil
		default:
			t.Fatalf("unexpected extra HTTP call: %d", calls)
			return nil, nil
		}
	}

	result, errEnvelope, errFetch := fetchKiroEvents(rawRequest)
	if errFetch != nil {
		t.Fatalf("fetch failed: %v", errFetch)
	}
	if errEnvelope != nil {
		t.Fatalf("unexpected error envelope: %s", errEnvelope)
	}
	if calls != 3 {
		t.Fatalf("expected two retries and one successful request, got %d calls", calls)
	}
	if result == nil || result.text != "recovered" || len(result.calls) != 0 {
		t.Fatalf("unexpected recovered result: %+v", result)
	}
}

func TestFetchKiroEventsStopsAfterEmptyResponseLimit(t *testing.T) {
	payload, err := json.Marshal(claudeRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []claudeMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
	})
	if err != nil {
		t.Fatalf("marshal Claude request: %v", err)
	}
	storage, err := json.Marshal(kiroCredential{AccessToken: "test-access-token"})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	rawRequest, err := json.Marshal(executorRequest{ExecutorRequest: pluginapi.ExecutorRequest{
		Model:       "claude-sonnet-4-5",
		Payload:     payload,
		StorageJSON: storage,
	}})
	if err != nil {
		t.Fatalf("marshal executor request: %v", err)
	}

	oldHTTPDo := kiroHTTPDo
	t.Cleanup(func() { kiroHTTPDo = oldHTTPDo })
	calls := 0
	kiroHTTPDo = func(req hostapi.HTTPRequest) (*hostapi.HTTPResponse, error) {
		calls++
		return &hostapi.HTTPResponse{StatusCode: 200}, nil
	}

	result, errEnvelope, errFetch := fetchKiroEvents(rawRequest)
	if errFetch != nil {
		t.Fatalf("fetch failed: %v", errFetch)
	}
	if errEnvelope != nil {
		t.Fatalf("unexpected error envelope: %s", errEnvelope)
	}
	if calls != maxEmptyResponseAttempts {
		t.Fatalf("expected %d attempts, got %d", maxEmptyResponseAttempts, calls)
	}
	if result == nil || result.text != "" || len(result.calls) != 0 {
		t.Fatalf("expected final empty result, got %+v", result)
	}
}

func TestNonStreamResponseUsesClientProtocolRepresentation(t *testing.T) {
	for _, format := range []string{"claude", "openai", "openai-response", ""} {
		t.Run(format, func(t *testing.T) {
			res := &kiroExecResult{text: "hello", calls: []toolCall{{id: "call", name: "read_file", input: json.RawMessage(`{"path":"x"}`)}}, model: "claude-sonnet-4-6", sourceFormat: format, requestPayload: []byte(`{"messages":[]}`)}
			raw, err := renderKiroNonStreamResponse(res)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				OK     bool                       `json:"ok"`
				Result pluginapi.ExecutorResponse `json:"result"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			if !envelope.OK {
				t.Fatalf("error envelope: %s", raw)
			}
			if format == "openai-response" {
				events := logicalStreamEvents(t, []executorStreamChunk{{Payload: envelope.Result.Payload}})
				if len(events) != 9 || sseEventType(events[0].Payload) != "message_start" || sseEventType(events[len(events)-1].Payload) != "message_stop" {
					t.Fatalf("incomplete Responses intermediate stream: %s", envelope.Result.Payload)
				}
			} else {
				var message claudeResponse
				if err := json.Unmarshal(envelope.Result.Payload, &message); err != nil {
					t.Fatal(err)
				}
				if len(message.Content) != 2 || message.StopReason != "tool_use" {
					t.Fatalf("JSON response changed: %+v", message)
				}
			}
		})
	}
}

func TestOriginalClientFormatSurvivesHostNativeRewrite(t *testing.T) {
	for _, tc := range []struct{ original, source, want string }{
		{`{"input":"hi"}`, "claude", "openai-response"},
		{`{"input":[]}`, "claude", "openai-response"},
		{`{"messages":[]}`, "claude", "claude"},
		{`{"messages":[]}`, "openai", "openai"},
		{"", "openai-response", "openai-response"},
		{"invalid", "claude", "claude"},
	} {
		req := executorRequest{ExecutorRequest: pluginapi.ExecutorRequest{SourceFormat: tc.source, OriginalRequest: []byte(tc.original)}}
		if got := originalClientFormat(req); got != tc.want {
			t.Fatalf("original=%q source=%q: got %q want %q", tc.original, tc.source, got, tc.want)
		}
	}
}

func TestNativeChatOutputPreservesTextToolsAndUsage(t *testing.T) {
	res := &kiroExecResult{text: "hello", calls: []toolCall{{id: "call", name: "read_file", input: json.RawMessage(`{"path":"x"}`)}}, model: "claude-sonnet-4-6", outputFormat: "openai", requestPayload: []byte(`{"messages":[]}`)}
	for _, stream := range []bool{false, true} {
		response := buildOpenAIChatResponse(res, stream)
		key, object := "message", "chat.completion"
		if stream {
			key, object = "delta", "chat.completion.chunk"
		}
		if response["object"] != object || response["model"] != res.model {
			t.Fatalf("bad response: %+v", response)
		}
		choice := response["choices"].([]any)[0].(map[string]any)
		message := choice[key].(map[string]any)
		if message["content"] != "hello" || choice["finish_reason"] != "tool_calls" {
			t.Fatalf("lost text/tools: %+v", choice)
		}
		call := message["tool_calls"].([]any)[0].(map[string]any)
		if call["id"] != "call" || call["function"].(map[string]any)["arguments"] != `{"path":"x"}` {
			t.Fatalf("bad tool: %+v", call)
		}
		if stream && call["index"] != 0 {
			t.Fatal("stream tool index missing")
		}
		usage := response["usage"].(map[string]any)
		if usage["completion_tokens"] != estimateOutputTokens(res.text, res.calls) {
			t.Fatal("usage changed")
		}
	}
	raw, err := renderKiroNonStreamResponse(res)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result pluginapi.ExecutorResponse `json:"result"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err = json.Unmarshal(envelope.Result.Payload, &response); err != nil {
		t.Fatal(err)
	}
	if response["object"] != "chat.completion" {
		t.Fatal("native chat returned Claude instead")
	}
}
