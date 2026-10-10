package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// 待办图片：先上传（未绑定），保存事项 / 子任务 / 进展 / 闭环时再按 ID 绑定。
const (
	TodoImageMaxBytes     = 5 << 20
	TodoImageMaxPerTarget = 9
	TodoImageOrphanTTL    = 24 * time.Hour
)

// 图片挂载位置：todo=事项本身，subtask=子任务（ref_id=子任务 ID），log=跟进记录（ref_id=日志 ID，用于进展 / 闭环）。
const (
	TodoImageScopeTodo    = "todo"
	TodoImageScopeSubtask = "subtask"
	TodoImageScopeLog     = "log"
)

// 只收位图；SVG 可内嵌脚本，不允许。
var todoImageMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

type TodoImage struct {
	ID        string `json:"id"`
	TodoID    string `json:"todo_id"`
	Scope     string `json:"scope"`
	RefID     string `json:"ref_id"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

// DetectTodoImageMime 按文件内容识别类型并校验大小，不信任客户端声明的 Content-Type。
func DetectTodoImageMime(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("图片为空")
	}
	if len(data) > TodoImageMaxBytes {
		return "", fmt.Errorf("图片不能超过 %dMB", TodoImageMaxBytes>>20)
	}
	mime := http.DetectContentType(data)
	if !todoImageMimes[mime] {
		return "", fmt.Errorf("仅支持 PNG / JPG / GIF / WebP 图片（识别为 %s）", mime)
	}
	return mime, nil
}

// SanitizeImageFilename 去掉路径，限制长度，空名兜底。
func SanitizeImageFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return "image"
	}
	if utf8.RuneCountInString(name) > 100 {
		r := []rune(name)
		name = string(r[len(r)-100:]) // 保留结尾，扩展名不丢
	}
	return name
}

// NormalizeTodoImageIDs 去空白、去重，并限制单处数量。
func NormalizeTodoImageIDs(ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > TodoImageMaxPerTarget {
		return nil, fmt.Errorf("每处最多 %d 张图片", TodoImageMaxPerTarget)
	}
	return out, nil
}

const todoImageCols = `id, todo_id, scope, ref_id, filename, mime, size_bytes, created_by, created_at`

func scanTodoImage(sc interface{ Scan(dest ...any) error }) (*TodoImage, error) {
	var m TodoImage
	if err := sc.Scan(&m.ID, &m.TodoID, &m.Scope, &m.RefID, &m.Filename, &m.Mime, &m.SizeBytes, &m.CreatedBy, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateTodoImage 保存一张未绑定的图片。
func (d *DB) CreateTodoImage(m *TodoImage, data []byte) error {
	mime, err := DetectTodoImageMime(data)
	if err != nil {
		return err
	}
	m.ID = uuid.NewString()
	m.TodoID, m.Scope, m.RefID = "", "", ""
	m.Filename = SanitizeImageFilename(m.Filename)
	m.Mime = mime
	m.SizeBytes = int64(len(data))
	m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT INTO tbl_todo_images (`+todoImageCols+`, data) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.TodoID, m.Scope, m.RefID, m.Filename, m.Mime, m.SizeBytes, m.CreatedBy, m.CreatedAt, data)
	return err
}

// GetTodoImageData 读取图片元数据与内容；不存在返回 nil。
func (d *DB) GetTodoImageData(id string) (*TodoImage, []byte, error) {
	var data []byte
	var m TodoImage
	err := d.QueryRow(`SELECT `+todoImageCols+`, data FROM tbl_todo_images WHERE id=?`, id).Scan(
		&m.ID, &m.TodoID, &m.Scope, &m.RefID, &m.Filename, &m.Mime, &m.SizeBytes, &m.CreatedBy, &m.CreatedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &m, data, nil
}

// ListTodoImages 事项下全部已绑定图片（不含内容），按上传时间排序。
func (d *DB) ListTodoImages(todoID string) ([]TodoImage, error) {
	rows, err := d.Query(`SELECT `+todoImageCols+` FROM tbl_todo_images WHERE todo_id=? ORDER BY created_at, id`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoImage{}
	for rows.Next() {
		m, err := scanTodoImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ListTodoImageIDs 某处（事项 / 子任务 / 日志）当前绑定的图片 ID。
func (d *DB) ListTodoImageIDs(todoID, scope, refID string) ([]string, error) {
	rows, err := d.Query(`SELECT id FROM tbl_todo_images WHERE todo_id=? AND scope=? AND ref_id=?`, todoID, scope, refID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CheckTodoImagesBindable 校验图片均为当前用户上传且尚未绑定；在写入事项前调用，避免半途失败。
func (d *DB) CheckTodoImagesBindable(ids []string, user string) error {
	ids, err := NormalizeTodoImageIDs(ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM tbl_todo_images WHERE id=? AND todo_id='' AND created_by=?`, id, user).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return errors.New("图片不存在、已过期或已被使用，请重新上传")
		}
	}
	return nil
}

// SyncTodoImages 让某处（事项 / 子任务 / 日志）的图片与 ids 一致：移除的删掉，新增的从未绑定图片中认领。
func (d *DB) SyncTodoImages(todoID, scope, refID string, ids []string, user string) error {
	ids, err := NormalizeTodoImageIDs(ids)
	if err != nil {
		return err
	}
	existing, err := d.ListTodoImageIDs(todoID, scope, refID)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, id := range existing {
		keep[id] = true
	}
	var added []string
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
		if !keep[id] {
			added = append(added, id)
		}
	}
	if err := d.CheckTodoImagesBindable(added, user); err != nil {
		return err
	}
	for _, id := range existing {
		if !want[id] {
			if _, err := d.Exec(`DELETE FROM tbl_todo_images WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	for _, id := range added {
		if _, err := d.Exec(`UPDATE tbl_todo_images SET todo_id=?, scope=?, ref_id=? WHERE id=? AND todo_id='' AND created_by=?`,
			todoID, scope, refID, id, user); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTodoImagesByRef 删除某处的全部图片（如删除子任务时）。
func (d *DB) DeleteTodoImagesByRef(todoID, scope, refID string) error {
	_, err := d.Exec(`DELETE FROM tbl_todo_images WHERE todo_id=? AND scope=? AND ref_id=?`, todoID, scope, refID)
	return err
}

// PurgeOrphanTodoImages 清理超过 TodoImageOrphanTTL 仍未绑定的图片（上传后取消了保存）。
func (d *DB) PurgeOrphanTodoImages(now time.Time) error {
	before := now.Add(-TodoImageOrphanTTL).UTC().Format(time.RFC3339)
	_, err := d.Exec(`DELETE FROM tbl_todo_images WHERE todo_id='' AND created_at < ?`, before)
	return err
}
