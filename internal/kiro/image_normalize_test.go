package kiro

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/xiaokui-dev/cliproxyapi-kiro-plugin/internal/hostapi"
)

func testPNG(t *testing.T, w, h int) cwImage {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return cwImage{Format: "png", Source: cwImageSource{Bytes: base64.StdEncoding.EncodeToString(b.Bytes())}}
}
func imageSize(t *testing.T, img cwImage) (int, int) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(img.Source.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c.Width, c.Height
}
func TestNormalizeImageBoundaries(t *testing.T) {
	for _, size := range [][4]int{{2000, 1000, 2000, 1000}, {2001, 1000, 2000, 999}, {1000, 2001, 999, 2000}, {8, 4, 8, 4}} {
		original := testPNG(t, size[0], size[1])
		result, err := normalizeCWImage(original, 2000)
		if err != nil {
			t.Fatal(err)
		}
		w, h := imageSize(t, result)
		if w != size[2] || h != size[3] {
			t.Fatalf("size %v became %dx%d", size, w, h)
		}
		if size[0] <= 2000 && size[1] <= 2000 && original != result {
			t.Fatal("small image changed")
		}
	}
	for _, data := range []string{"not base64!", base64.StdEncoding.EncodeToString([]byte("not an image"))} {
		if _, err := normalizeCWImage(cwImage{Source: cwImageSource{Bytes: data}}, 2000); err == nil {
			t.Fatal("invalid accepted")
		}
	}
	// Reject oversized declared dimensions before allocating/decoding pixels.
	big := testPNG(t, 8193, 1)
	result, err := normalizeCWImage(big, 8000)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := imageSize(t, result)
	if w != 8000 {
		t.Fatal(w)
	}
}
func TestNormalizeHistoryThreshold(t *testing.T) {
	old := testPNG(t, 2100, 1050)
	req := &cwRequest{}
	req.ConversationState.History = []cwHistoryItem{{UserInputMessage: &cwUserInputMessage{Images: []cwImage{old}}}}
	for i := 0; i < 19; i++ {
		req.ConversationState.CurrentMessage.UserInputMessage.Images = append(req.ConversationState.CurrentMessage.UserInputMessage.Images, testPNG(t, 1, 1))
	}
	if err := normalizeCWImages(req); err != nil {
		t.Fatal(err)
	}
	if req.ConversationState.History[0].UserInputMessage.Images[0] != old {
		t.Fatal("20-image request lost resolution")
	}
	req.ConversationState.CurrentMessage.UserInputMessage.Images = append(req.ConversationState.CurrentMessage.UserInputMessage.Images, testPNG(t, 1, 1))
	if err := normalizeCWImages(req); err != nil {
		t.Fatal(err)
	}
	w, h := imageSize(t, req.ConversationState.History[0].UserInputMessage.Images[0])
	if w != 2000 || h != 1000 {
		t.Fatalf("history not resized: %dx%d", w, h)
	}
}
func TestExecutorNormalizesHistoricalToolImages(t *testing.T) {
	large := testPNG(t, 2100, 1050)
	tiny := testPNG(t, 1, 1)
	block := func(img cwImage) map[string]any {
		return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": img.Source.Bytes}}
	}
	history, _ := json.Marshal([]any{map[string]any{"type": "tool_result", "tool_use_id": "call1", "content": []any{block(large)}}})
	current := []any{map[string]any{"type": "text", "text": "read history"}}
	for i := 0; i < 20; i++ {
		current = append(current, block(tiny))
	}
	content, _ := json.Marshal(current)
	payload, _ := json.Marshal(claudeRequest{Model: "claude-opus-5.5", Messages: []claudeMessage{{Role: "user", Content: history}, {Role: "assistant", Content: json.RawMessage(`"okay"`)}, {Role: "user", Content: content}}})
	storage, _ := json.Marshal(kiroCredential{AccessToken: "test"})
	raw, _ := json.Marshal(executorRequest{ExecutorRequest: pluginapi.ExecutorRequest{Payload: payload, StorageJSON: storage, Model: "claude-opus-5.5"}})
	old := kiroHTTPDo
	t.Cleanup(func() { kiroHTTPDo = old })
	calls := 0
	kiroHTTPDo = func(r hostapi.HTTPRequest) (*hostapi.HTTPResponse, error) {
		calls++
		var req cwRequest
		if err := json.Unmarshal(r.Body, &req); err != nil {
			t.Fatal(err)
		}
		count := 0
		check := func(images []cwImage) {
			for _, img := range images {
				count++
				w, h := imageSize(t, img)
				if w > 2000 || h > 2000 {
					t.Fatal("unsafe outbound image")
				}
			}
		}
		check(req.ConversationState.CurrentMessage.UserInputMessage.Images)
		for _, item := range req.ConversationState.History {
			if item.UserInputMessage != nil {
				check(item.UserInputMessage.Images)
			}
		}
		if count != 21 {
			t.Fatalf("images lost: %d", count)
		}
		return &hostapi.HTTPResponse{StatusCode: 200, Body: encodeFrame("assistantResponseEvent", `{"content":"ok"}`)}, nil
	}
	_, envelope, err := fetchKiroEvents(raw)
	if err != nil || envelope != nil || calls != 1 {
		t.Fatalf("fetch: %v %s %d", err, envelope, calls)
	}
}

func TestImageDecodeAndByteBounds(t *testing.T) {
	src := testPNG(t, 1, 1)
	raw, _ := base64.StdEncoding.DecodeString(src.Source.Bytes)
	binary.BigEndian.PutUint32(raw[16:20], 100000)
	binary.BigEndian.PutUint32(raw[20:24], 100000)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	src.Source.Bytes = base64.StdEncoding.EncodeToString(raw)
	if _, err := normalizeCWImage(src, 2000); err == nil {
		t.Fatal("unbounded pixels accepted")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 1200))
	r := rand.New(rand.NewSource(1))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = byte(r.Intn(256))
		img.Pix[i+1] = byte(r.Intn(256))
		img.Pix[i+2] = byte(r.Intn(256))
		img.Pix[i+3] = 255
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	src = cwImage{Format: "png", Source: cwImageSource{Bytes: base64.StdEncoding.EncodeToString(b.Bytes())}}
	if len(src.Source.Bytes) <= maxImageBase64Bytes {
		t.Fatal("fixture too small")
	}
	out, err := normalizeCWImage(src, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Source.Bytes) > maxImageBase64Bytes {
		t.Fatal("byte limit exceeded")
	}
	w, h := imageSize(t, out)
	if w >= 1200 || h >= 1200 {
		t.Fatal("byte reduction not applied")
	}
}
func TestSupportedImageFormats(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 5))
	for _, format := range []string{"jpeg", "gif"} {
		var b bytes.Buffer
		if format == "jpeg" {
			jpeg.Encode(&b, img, nil)
		} else {
			gif.Encode(&b, img, nil)
		}
		src := cwImage{Format: format, Source: cwImageSource{Bytes: base64.StdEncoding.EncodeToString(b.Bytes())}}
		out, err := normalizeCWImage(src, 2000)
		if err != nil || out != src {
			t.Fatalf("%s: %v", format, err)
		}
	}
}
