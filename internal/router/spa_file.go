package router

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// 这些类型压缩收益大。图片和字体本身已压缩，不在这里再压。
func spaGzipExt(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".js", ".mjs", ".css", ".html", ".svg":
		return true
	default:
		return false
	}
}

func requestAcceptsGzip(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			name = strings.TrimSpace(part[:i])
			for _, param := range strings.Split(part[i+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(strings.ToLower(param), "q=") {
					if v, err := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64); err == nil {
						q = v
					}
				}
			}
		}
		if strings.EqualFold(name, "gzip") && q > 0 {
			return true
		}
	}
	return false
}

type spaGzipEntry struct {
	mod  int64
	size int64
	body []byte
}

var spaGzipCache sync.Map

func cachedGzip(full string, info os.FileInfo) ([]byte, error) {
	mod := info.ModTime().UnixNano()
	size := info.Size()
	if v, ok := spaGzipCache.Load(full); ok {
		ent := v.(spaGzipEntry)
		if ent.mod == mod && ent.size == size {
			return ent.body, nil
		}
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	body := append([]byte(nil), buf.Bytes()...)
	spaGzipCache.Store(full, spaGzipEntry{mod: mod, size: size, body: body})
	return body, nil
}

func serveSPAFile(c *gin.Context, full, urlPath string, info os.FileInfo) {
	if strings.HasPrefix(urlPath, "/assets/") {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else if strings.EqualFold(filepath.Base(full), "index.html") {
		c.Header("Cache-Control", "no-cache")
	}
	if spaGzipExt(full) {
		c.Header("Vary", "Accept-Encoding")
	}
	if c.GetHeader("Range") != "" || !spaGzipExt(full) || !requestAcceptsGzip(c.Request) {
		c.File(full)
		return
	}
	gz, err := cachedGzip(full, info)
	if err != nil || len(gz) == 0 || len(gz) >= int(info.Size()) {
		c.File(full)
		return
	}
	c.Header("Content-Encoding", "gzip")
	ctype := mime.TypeByExtension(filepath.Ext(full))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	c.Data(http.StatusOK, ctype, gz)
}
