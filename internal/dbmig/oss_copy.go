package dbmig

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/ossupload"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type ossChunk struct {
	Key  string `json:"key"`
	Rows int64  `json:"rows"`
	Size int64  `json:"bytes"`
	ETag string `json:"etag,omitempty"`
}

type ossManifestObject struct {
	ObjectID    string     `json:"object_id"`
	SourceDB    string     `json:"source_db_name"`
	SourceTable string     `json:"source_table_name"`
	TargetDB    string     `json:"target_db_name"`
	TargetTable string     `json:"target_table_name"`
	Chunks      []ossChunk `json:"chunks"`
}

func openMigrationOSS(st *store.DB, secretKey string, t *store.MigrationTask) (*ossupload.Client, string, int, error) {
	if st == nil || t == nil {
		return nil, "", 0, fmt.Errorf("OSS 中转不可用")
	}
	env := strings.TrimSpace(t.SourceEnv)
	if env == "" {
		env = strings.TrimSpace(t.Env)
	}
	plain, err := st.GetOSSSettingPlainByEnvInTenant(env, st.TenantOfInstance(t.SourceID))
	if err != nil {
		return nil, "", 0, err
	}
	if plain == nil || !plain.Configured {
		return nil, "", 0, fmt.Errorf("请先在配置中心为环境 %s 配置 OSS", env)
	}
	sk, err := secret.Decrypt(secretKey, plain.SecretEnc)
	if err != nil {
		return nil, "", 0, fmt.Errorf("OSS 密钥解密失败")
	}
	cli, err := ossupload.NewClient(plain.Endpoint, plain.Bucket, plain.AccessKey, sk, plain.CloudProduct)
	if err != nil {
		return nil, "", 0, err
	}
	chunkMB := plain.ChunkSizeMB
	if chunkMB <= 0 {
		chunkMB = 100
	}
	prefix := strings.Trim(strings.TrimSpace(plain.Prefix), "/")
	return cli, prefix, chunkMB, nil
}

func copyPostgresViaOSS(ctx context.Context, st *store.DB, cli *ossupload.Client, prefix string, chunkMB int, manifestMu *sync.Mutex, t *store.MigrationTask, src, tgt *sql.DB, o *store.MigrationObject, batchSize, speedLimit int, onBatch func(n int64) error) (int64, error) {
	if cli == nil || o == nil {
		return 0, fmt.Errorf("OSS 中转缺少连接或对象")
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	if chunkMB <= 0 {
		chunkMB = 100
	}
	chunkMax := int64(chunkMB) * 1024 * 1024
	cols, err := listColumns(ctx, src, "postgres", firstNonEmpty(o.SourceSchema, "public"), o.SourceTable)
	if err != nil {
		return 0, err
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("源表 %s 没有可迁移列", o.SourceTable)
	}
	overriding, err := pgIdentityAlways(ctx, tgt, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	if err != nil {
		return 0, err
	}
	if manifestMu != nil {
		manifestMu.Lock()
	}
	clearOSSObject(ctx, st, cli, t.ID, o.ID)
	if manifestMu != nil {
		manifestMu.Unlock()
	}
	folder := strings.Trim(prefix, "/")
	if folder != "" {
		folder += "/"
	}
	folder += "migration/" + t.ID + "/" + o.ID
	srcRef := tableRef("postgres", firstNonEmpty(o.SourceSchema, "public"), o.SourceTable)
	q := fmt.Sprintf("SELECT %s FROM %s", joinQuoted("postgres", cols), srcRef)
	rows, err := src.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(cols); err != nil {
		return 0, err
	}
	var chunks []ossChunk
	var rowsInChunk int64
	var total int64
	chunkIdx := 0
	upload := func() error {
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if rowsInChunk == 0 {
			return nil
		}
		chunkIdx++
		key := folder + fmt.Sprintf("/chunk_%d.csv", chunkIdx)
		body := append([]byte(nil), buf.Bytes()...)
		etag, err := cli.PutObject(ctx, key, "text/csv; charset=utf-8", body)
		if err != nil {
			return err
		}
		chunks = append(chunks, ossChunk{Key: key, Rows: rowsInChunk, Size: int64(len(body)), ETag: etag})
		buf.Reset()
		w = csv.NewWriter(&buf)
		if err := w.Write(cols); err != nil {
			return err
		}
		rowsInChunk = 0
		return nil
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return total, err
		}
		rec := make([]string, len(cols))
		for i, v := range vals {
			rec[i] = ossCell(v)
		}
		if err := w.Write(rec); err != nil {
			return total, err
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return total, err
		}
		rowsInChunk++
		total++
		if int64(buf.Len()) >= chunkMax {
			if err := upload(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if err := upload(); err != nil {
		return total, err
	}
	if manifestMu != nil {
		manifestMu.Lock()
	}
	err = saveOSSManifest(st, t.ID, ossManifestObject{
		ObjectID: o.ID, SourceDB: o.SourceDB, SourceTable: o.SourceTable,
		TargetDB: o.TargetDB, TargetTable: o.TargetTable, Chunks: chunks,
	})
	if manifestMu != nil {
		manifestMu.Unlock()
	}
	if err != nil {
		return total, err
	}
	var loaded int64
	for _, ch := range chunks {
		rc, _, err := cli.GetObject(ctx, ch.Key)
		if err != nil {
			return loaded, err
		}
		n, err := importOSSChunk(ctx, tgt, o, cols, overriding, rc, batchSize, speedLimit, onBatch)
		_ = rc.Close()
		if err != nil {
			return loaded, err
		}
		loaded += n
	}
	for _, ch := range chunks {
		if err := cli.DeleteObject(ctx, ch.Key); err != nil {
			log.Printf("migration %s object %s: data loaded, delete oss %s: %v", t.ID, o.ID, ch.Key, err)
		}
	}
	return loaded, nil
}

func clearOSSObject(ctx context.Context, st *store.DB, cli *ossupload.Client, taskID, objectID string) {
	if st == nil || cli == nil || objectID == "" {
		return
	}
	raw, _ := st.GetMigrationTags(taskID)["oss_file_manifest"].([]any)
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok || fmt.Sprint(m["object_id"]) != objectID {
			continue
		}
		chunks, _ := m["chunks"].([]any)
		for _, c := range chunks {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			key := strings.TrimSpace(fmt.Sprint(cm["key"]))
			if key == "" || key == "<nil>" {
				continue
			}
			_ = cli.DeleteObject(ctx, key)
		}
	}
}

func importOSSChunk(ctx context.Context, tgt *sql.DB, o *store.MigrationObject, cols []string, overriding bool, r io.Reader, batchSize, speedLimit int, onBatch func(n int64) error) (int64, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return 0, err
	}
	if len(header) != len(cols) {
		return 0, fmt.Errorf("OSS 分片列数与源表不一致")
	}
	var batch [][]any
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n := int64(len(batch))
		if err := insertBatch(ctx, tgt, "postgres", o, cols, batch, overriding); err != nil {
			return err
		}
		total += n
		if onBatch != nil {
			if err := onBatch(n); err != nil {
				return err
			}
		}
		if speedLimit > 0 {
			d := time.Duration(float64(time.Second) * float64(n) / float64(speedLimit))
			if d > 0 {
				timer := time.NewTimer(d)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		batch = batch[:0]
		return nil
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
		row := make([]any, len(cols))
		for i := range cols {
			cell := ""
			if i < len(rec) {
				cell = rec[i]
			}
			if cell == `\N` {
				row[i] = nil
			} else {
				row[i] = cell
			}
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

func ossCell(v any) string {
	if v == nil {
		return `\N`
	}
	switch x := v.(type) {
	case []byte:
		if x == nil {
			return `\N`
		}
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(x)
	}
}

func saveOSSManifest(st *store.DB, taskID string, obj ossManifestObject) error {
	if st == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	cur := st.GetMigrationTags(taskID)
	list := []any{}
	if raw, ok := cur["oss_file_manifest"].([]any); ok {
		for _, it := range raw {
			m, ok := it.(map[string]any)
			if !ok || fmt.Sprint(m["object_id"]) == obj.ObjectID {
				continue
			}
			list = append(list, it)
		}
	}
	list = append(list, map[string]any{
		"object_id":         obj.ObjectID,
		"source_db_name":    obj.SourceDB,
		"source_table_name": obj.SourceTable,
		"target_db_name":    obj.TargetDB,
		"target_table_name": obj.TargetTable,
		"chunks":            obj.Chunks,
	})
	next := store.MergeMigrationTags(cur, map[string]any{"oss_file_manifest": list})
	return st.SetMigrationTags(taskID, next)
}
