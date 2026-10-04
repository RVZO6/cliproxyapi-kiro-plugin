package kiro

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const maxImageBase64Bytes = 5 * 1024 * 1024 // Conservative partner-transport cap.
const maxImageInputBytes = 16 * 1024 * 1024
const maxImageDecodePixels = 64 * 1024 * 1024

// Normalize the complete outbound request, never persisted history or source files.
// Claude's many-image threshold counts historical and tool-result images too.
func normalizeCWImages(req *cwRequest) error {
	messages := []*cwUserInputMessage{&req.ConversationState.CurrentMessage.UserInputMessage}
	count := len(messages[0].Images)
	for _, item := range req.ConversationState.History {
		if item.UserInputMessage != nil {
			messages = append(messages, item.UserInputMessage)
			count += len(item.UserInputMessage.Images)
		}
	}
	edge := 8000
	if count > 20 {
		edge = 2000
	}
	cache := make(map[[32]byte]cwImage)
	for mi, msg := range messages {
		for ii, img := range msg.Images {
			key := sha256.Sum256([]byte(img.Source.Bytes))
			normalized, ok := cache[key]
			if !ok {
				var err error
				normalized, err = normalizeCWImage(img, edge)
				if err != nil {
					return fmt.Errorf("outbound image %d in message %d: %w", ii+1, mi+1, err)
				}
				cache[key] = normalized
			}
			msg.Images[ii] = normalized
		}
	}
	return nil
}

func normalizeCWImage(src cwImage, edge int) (cwImage, error) {
	if len(src.Source.Bytes) > maxImageInputBytes {
		return cwImage{}, fmt.Errorf("encoded image exceeds 16 MiB normalization safety limit")
	}
	raw, err := base64.StdEncoding.DecodeString(src.Source.Bytes)
	if err != nil {
		return cwImage{}, fmt.Errorf("invalid base64 image")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return cwImage{}, fmt.Errorf("unsupported or invalid image data")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImageDecodePixels {
		return cwImage{}, fmt.Errorf("image exceeds 64-megapixel decoding safety limit")
	}
	src.Format = format
	if cfg.Width <= edge && cfg.Height <= edge && len(src.Source.Bytes) <= maxImageBase64Bytes {
		return src, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return cwImage{}, fmt.Errorf("cannot decode image")
	}
	w, h := fitImageDimensions(cfg.Width, cfg.Height, edge)
	for {
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return cwImage{}, fmt.Errorf("cannot encode resized image")
		}
		if base64.StdEncoding.EncodedLen(buf.Len()) <= maxImageBase64Bytes {
			return cwImage{Format: "png", Source: cwImageSource{Bytes: base64.StdEncoding.EncodeToString(buf.Bytes())}}, nil
		}
		if w == 1 && h == 1 {
			return cwImage{}, fmt.Errorf("image cannot fit transport byte limit")
		}
		w, h = max(1, w*3/4), max(1, h*3/4)
	}
}

func fitImageDimensions(w, h, edge int) (int, int) {
	if w <= edge && h <= edge {
		return w, h
	}
	if w >= h {
		return edge, max(1, int(int64(h)*int64(edge)/int64(w)))
	}
	return max(1, int(int64(w)*int64(edge)/int64(h))), edge
}
