package sqlexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
)

// ErrMetaUnsupported 表示执行器没有 /v1/sql/meta（版本过旧），与 jobs 的 ErrJobNotFound 区分。
var ErrMetaUnsupported = errors.New("执行器版本过旧，不支持元数据接口 /v1/sql/meta；请到「配置 → 执行器」用当前平台安装包重新下发 sql-agent 后再试")

// MetaRequest 同步只读元数据：同实例下多库一次拉取 schema 表与 publication 成员。
type MetaRequest struct {
	Target    Target   `json:"target"`
	Databases []MetaDB `json:"databases"`
}

type MetaDB struct {
	DBName          string `json:"db_name"`
	Schema          string `json:"schema"`
	PublicationName string `json:"publication_name"`
}

type MetaDBResult struct {
	DBName            string   `json:"db_name"`
	Tables            []string `json:"tables"`
	PublicationTables []string `json:"publication_tables"`
	Error             string   `json:"error,omitempty"`
}

type MetaResponse struct {
	Results []MetaDBResult `json:"results"`
}

const metaMaxDatabases = 128

// FetchMeta 调用执行器同步元数据接口（跨库批量，超时放宽到 90s）。
// 旧执行器无此路由时返回 404，映射为 ErrMetaUnsupported（不套用 ErrJobNotFound）。
func FetchMeta(ctx context.Context, baseURL, user, pass string, req MetaRequest) (MetaResponse, error) {
	var out MetaResponse
	err := callWithTimeout(ctx, 90*time.Second, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/sql/meta", user, pass, req, &out)
	if err != nil && errors.Is(err, ErrJobNotFound) {
		return out, ErrMetaUnsupported
	}
	return out, err
}

// RunMeta 在执行器进程内就近连库查询（仅目录 SQL）。
func RunMeta(ctx context.Context, req MetaRequest) (MetaResponse, error) {
	host := strings.TrimSpace(req.Target.Host)
	if host == "" {
		return MetaResponse{}, fmt.Errorf("target.host 必填")
	}
	if len(req.Databases) == 0 {
		return MetaResponse{}, fmt.Errorf("databases 必填")
	}
	if len(req.Databases) > metaMaxDatabases {
		return MetaResponse{}, fmt.Errorf("单次最多 %d 个库", metaMaxDatabases)
	}
	port := req.Target.Port
	if port <= 0 {
		port = 5432
	}
	dbType := strings.TrimSpace(req.Target.DBType)
	if dbType == "" {
		dbType = "postgres"
	}
	out := MetaResponse{Results: make([]MetaDBResult, 0, len(req.Databases))}
	for _, item := range req.Databases {
		dbName := strings.TrimSpace(item.DBName)
		res := MetaDBResult{DBName: dbName, Tables: []string{}, PublicationTables: []string{}}
		if dbName == "" {
			res.Error = "db_name 必填"
			out.Results = append(out.Results, res)
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		db, err := dbmig.Open(cctx, dbType, host, port, req.Target.Username, req.Target.Password, dbName)
		if err != nil {
			cancel()
			res.Error = err.Error()
			out.Results = append(out.Results, res)
			continue
		}
		schema := strings.TrimSpace(item.Schema)
		if schema == "" {
			schema = "public"
		}
		tables, terr := listBaseTables(cctx, db, schema)
		if terr != nil {
			db.Close()
			cancel()
			res.Error = terr.Error()
			out.Results = append(out.Results, res)
			continue
		}
		res.Tables = tables
		if pub := strings.TrimSpace(item.PublicationName); pub != "" {
			pubs, perr := listPublicationTables(cctx, db, pub)
			if perr != nil {
				db.Close()
				cancel()
				res.Error = perr.Error()
				out.Results = append(out.Results, res)
				continue
			}
			res.PublicationTables = pubs
		}
		db.Close()
		cancel()
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func listBaseTables(ctx context.Context, db *sql.DB, schema string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" && !strings.EqualFold(n, "debezium_signal") {
			all = append(all, n)
		}
	}
	return all, rows.Err()
}

func listPublicationTables(ctx context.Context, db *sql.DB, pub string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname=$1 ORDER BY schemaname, tablename`, pub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sch, tb string
		if err := rows.Scan(&sch, &tb); err != nil {
			return nil, err
		}
		if sch != "" && sch != "public" {
			out = append(out, sch+"."+tb)
		} else {
			out = append(out, tb)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}
