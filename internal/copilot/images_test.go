package copilot

import (
	"encoding/base64"
	"testing"
)

func TestNormalizeChatImages(t *testing.T) {
	raw := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	b64 := base64.StdEncoding.EncodeToString(raw)
	out, err := normalizeChatImages([]ChatImage{
		{Data: "data:image/png;base64," + b64, MimeType: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].MimeType != "image/png" || out[0].Data != b64 {
		t.Fatalf("%+v", out)
	}
}
