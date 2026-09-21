package flashback

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

func instHost(inst *store.Instance) string {
	h := strings.TrimSpace(inst.Host)
	if h == "" {
		h = strings.TrimSpace(inst.DomainName)
	}
	return h
}

func instanceToDomain(inst *store.Instance) *DomainInstance {
	if inst == nil {
		return nil
	}
	return &DomainInstance{
		ID: inst.ID, InstanceID: inst.ID, MainIP: instHost(inst), DomainName: inst.DomainName,
		DbType: inst.DBType, Port: inst.Port,
	}
}

func instanceToResource(inst *store.Instance) *ResourceDbsInfo {
	if inst == nil {
		return nil
	}
	tags := map[string]interface{}{}
	for _, raw := range inst.Tags {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if i := strings.IndexByte(s, ':'); i > 0 && i < len(s)-1 {
			tags[strings.TrimSpace(s[:i])] = strings.TrimSpace(s[i+1:])
			continue
		}
		if i := strings.IndexByte(s, '='); i > 0 && i < len(s)-1 {
			tags[strings.TrimSpace(s[:i])] = strings.TrimSpace(s[i+1:])
			continue
		}
		tags[s] = s
	}
	if inst.CloudVendor != "" {
		tags["flash_vendor"] = inst.CloudVendor
	}
	if inst.CloudInstanceID != "" {
		tags["pg"] = inst.CloudInstanceID
		tags["cloud_instance_id"] = inst.CloudInstanceID
	}
	if inst.CloudRegion != "" {
		tags["flash_region"] = inst.CloudRegion
	}
	return &ResourceDbsInfo{
		Address: fmt.Sprintf("%s:%d", instHost(inst), inst.Port),
		Name:    inst.Name, DbType: inst.DBType, Tags: tags,
		RegionID: inst.CloudRegion,
		Region:   &ResourceRegion{Region: inst.CloudRegion, Name: inst.CloudRegion},
	}
}

func flashbackConnectTarget(ctx context.Context, instanceID, dbName string) (*sql.DB, *DomainInstance, *ResourceDbsInfo, error) {
	if runtime == nil || runtime.Store == nil {
		return nil, nil, nil, fmt.Errorf("flashback runtime 未初始化")
	}
	inst, err := runtime.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return nil, nil, nil, fmt.Errorf("instance not found")
	}
	dom := instanceToDomain(inst)
	res := instanceToResource(inst)
	plain, err := secret.Decrypt(runtime.Secret, inst.Password)
	if err != nil {
		return nil, dom, res, fmt.Errorf("解密实例密码失败: %w", err)
	}
	host := instHost(inst)
	switch {
	case flashbackIsMySQL(inst.DBType):
		db, err := openMySQL(ctx, host, inst.Port, inst.Username, plain, dbName)
		return db, dom, res, err
	case flashbackIsPostgres(inst.DBType):
		db, err := openPostgres(ctx, host, inst.Port, inst.Username, plain, firstNonEmpty(dbName, "postgres"))
		return db, dom, res, err
	default:
		return nil, dom, res, fmt.Errorf("仅支持 PostgreSQL / MySQL 实例，当前类型 %s", inst.DBType)
	}
}

func connectSourcePG(ctx context.Context, instanceID, dbName, fallbackHost string, fallbackPort int) (*sql.DB, *ResourceDbsInfo, error) {
	db, _, res, err := flashbackConnectTarget(ctx, instanceID, dbName)
	if err != nil && fallbackHost != "" {
		if runtime == nil {
			return nil, res, err
		}
		inst, _ := runtime.Store.GetInstance(instanceID)
		plain := ""
		user := ""
		if inst != nil {
			plain, _ = secret.Decrypt(runtime.Secret, inst.Password)
			user = inst.Username
			if res == nil {
				res = instanceToResource(inst)
			}
		}
		db, err = openPostgres(ctx, fallbackHost, fallbackPort, user, plain, firstNonEmpty(dbName, "postgres"))
		return db, res, err
	}
	return db, res, err
}

func flashbackResolveMySQLCreds(ctx context.Context, instanceID, dbName, fallbackHost string, fallbackPort int) (flashbackMySQLCreds, *ResourceDbsInfo, error) {
	creds := flashbackMySQLCreds{Host: fallbackHost, Port: fallbackPort, DBName: dbName}
	var res *ResourceDbsInfo
	if runtime != nil && runtime.Store != nil {
		if inst, _ := runtime.Store.GetInstance(instanceID); inst != nil {
			creds.Host = instHost(inst)
			creds.Port = inst.Port
			creds.User = inst.Username
			creds.Password, _ = secret.Decrypt(runtime.Secret, inst.Password)
			creds.DBName = firstNonEmpty(dbName, creds.DBName)
			res = instanceToResource(inst)
		}
	}
	return creds, res, nil
}

func connectSourceMySQL(ctx context.Context, instanceID, dbName, fallbackHost string, fallbackPort int) (*sql.DB, *ResourceDbsInfo, flashbackMySQLCreds, error) {
	db, _, res, err := flashbackConnectTarget(ctx, instanceID, dbName)
	creds := flashbackMySQLCreds{Host: fallbackHost, Port: fallbackPort, DBName: dbName}
	if runtime != nil {
		if inst, _ := runtime.Store.GetInstance(instanceID); inst != nil {
			creds.Host = instHost(inst)
			creds.Port = inst.Port
			creds.User = inst.Username
			creds.Password, _ = secret.Decrypt(runtime.Secret, inst.Password)
		}
	}
	return db, res, creds, err
}

func openPostgres(ctx context.Context, host string, port int, user, password, dbName string) (*sql.DB, error) {
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("未配置连接主机或端口")
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "12")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func openMySQL(ctx context.Context, host string, port int, user, password, dbName string) (*sql.DB, error) {
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("未配置连接主机或端口")
	}
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = dbName
	cfg.Timeout = 12 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
