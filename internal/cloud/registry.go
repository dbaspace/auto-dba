package cloud

import "fmt"

const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
	EngineRedis    = "redis"
	EngineKafka    = "kafka"
	EngineHost     = "host"
)

var readers = map[string]Reader{}
var writers = map[string]Writer{}

// NormalizePGMemoryMB 把仍按 GB 存下来的 PostgreSQL 内存换成 MB。
// 规格接口和重新同步后的值已经是 MB，不小于 1024 的不再乘。
func NormalizePGMemoryMB(memoryMB int) int {
	if memoryMB <= 0 || memoryMB >= 1024 {
		return memoryMB
	}
	return memoryMB * 1024
}

func regKey(vendor, engine string) string {
	return vendor + "/" + engine
}

// Register 按厂商+引擎注册。同一厂商可有 postgres/mysql/redis/host 多套实现。
func Register(engine string, r Reader, w Writer) {
	if r != nil {
		readers[regKey(r.Vendor(), engine)] = r
	}
	if w != nil {
		writers[regKey(w.Vendor(), engine)] = w
	}
}

func GetReader(vendor, engine string) (Reader, error) {
	if engine == "" {
		engine = EnginePostgres
	}
	r, ok := readers[regKey(vendor, engine)]
	if !ok || r == nil {
		return nil, fmt.Errorf("%w：%s/%s", ErrUnsupported, vendor, engine)
	}
	return r, nil
}

func GetWriter(vendor, engine string) (Writer, error) {
	if engine == "" {
		engine = EnginePostgres
	}
	w, ok := writers[regKey(vendor, engine)]
	if !ok || w == nil {
		return nil, fmt.Errorf("%w：%s/%s", ErrUnsupported, vendor, engine)
	}
	return w, nil
}

func Supported(vendor, engine string) bool {
	if engine == "" {
		engine = EnginePostgres
	}
	_, ok := readers[regKey(vendor, engine)]
	return ok
}

func Engines() []string {
	return []string{EnginePostgres, EngineMySQL, EngineRedis, EngineKafka, EngineHost}
}
