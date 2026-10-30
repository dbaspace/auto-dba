package dqlexport

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
)

// GzipContentType 压缩导出文件的 MIME。
const GzipContentType = "application/gzip"

// GzipFileName 在原扩展名后追加 .gz（如 json → json.gz）。
func GzipFileName(baseName string) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		return "export.gz"
	}
	if strings.HasSuffix(strings.ToLower(baseName), ".gz") {
		return baseName
	}
	return baseName + ".gz"
}

// GzipExt 将 Encode 返回的扩展名转为带 gzip 的后缀（json → json.gz）。
func GzipExt(ext string) string {
	ext = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(ext)), ".")
	if ext == "" {
		ext = "bin"
	}
	if strings.HasSuffix(ext, ".gz") {
		return ext
	}
	return ext + ".gz"
}

// WriteGzipFile 将 body 以 gzip 写入 path（应为 *.gz），经临时文件 rename，返回压缩后字节数。
func WriteGzipFile(path string, body []byte) (int64, error) {
	path = filepath.Clean(path)
	tmp := path + ".writing"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	gw := gzip.NewWriter(f)
	if _, err := gw.Write(body); err != nil {
		_ = gw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := gw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
