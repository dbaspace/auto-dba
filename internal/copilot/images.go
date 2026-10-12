package copilot

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	maxChatImages     = 5
	maxChatImageBytes = 15 << 20 // 15MB，与 Cursor Agents 文档一致
)

var allowedImageMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/gif":  true,
	"image/webp": true,
}

// normalizeChatImages 清洗 base64 / mime，限制数量与大小。
func normalizeChatImages(in []ChatImage) ([]ChatImage, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxChatImages {
		return nil, fmt.Errorf("最多上传 %d 张图片", maxChatImages)
	}
	var out []ChatImage
	for i, im := range in {
		data := strings.TrimSpace(im.Data)
		mime := strings.ToLower(strings.TrimSpace(im.MimeType))
		if strings.HasPrefix(data, "data:") {
			// data:image/png;base64,XXXX
			rest := data[5:]
			semi := strings.Index(rest, ";")
			comma := strings.Index(rest, ",")
			if comma < 0 {
				return nil, fmt.Errorf("第 %d 张图片 data URL 无效", i+1)
			}
			if mime == "" && semi > 0 {
				mime = strings.ToLower(strings.TrimSpace(rest[:semi]))
			}
			data = rest[comma+1:]
		}
		data = strings.ReplaceAll(data, "\n", "")
		data = strings.ReplaceAll(data, "\r", "")
		data = strings.ReplaceAll(data, " ", "")
		if data == "" {
			return nil, fmt.Errorf("第 %d 张图片数据为空", i+1)
		}
		if mime == "image/jpg" {
			mime = "image/jpeg"
		}
		if mime == "" {
			mime = "image/png"
		}
		if !allowedImageMIME[mime] {
			return nil, fmt.Errorf("第 %d 张图片类型不支持：%s（仅 png/jpeg/gif/webp）", i+1, mime)
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			// 尝试 RawStdEncoding
			raw, err = base64.RawStdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("第 %d 张图片 base64 无效", i+1)
			}
			data = base64.StdEncoding.EncodeToString(raw)
		}
		if len(raw) > maxChatImageBytes {
			return nil, fmt.Errorf("第 %d 张图片超过 15MB", i+1)
		}
		out = append(out, ChatImage{Data: data, MimeType: mime})
	}
	return out, nil
}
