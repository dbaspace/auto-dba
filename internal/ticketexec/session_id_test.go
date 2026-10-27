package ticketexec

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"sync"
	"testing"
	"time"
)

func TestSessionQuerierAcceptedByDBAndTx(t *testing.T) {
	var _ sessionQuerier = (*sql.DB)(nil)
	var _ sessionQuerier = (*sql.Tx)(nil)
}

// 回归：MaxOpenConns(1) 下 BeginTx 后再对 *sql.DB 查会话号会占死连接池；
// 必须在 *sql.Tx 上查，否则 runPostgresDML 永远走不到 Exec/Commit。
func TestQuerySessionIDOnTxWithMaxOpenOne(t *testing.T) {
	name := "ticketexec_session_" + t.Name()
	sql.Register(name, &oneConnDriver{})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	done := make(chan int64, 1)
	go func() {
		done <- querySessionID(ctx, db, "postgres")
	}()
	select {
	case <-done:
		t.Fatal("querySessionID on *sql.DB after BeginTx should block with MaxOpenConns(1)")
	case <-time.After(150 * time.Millisecond):
	}

	sid := querySessionID(ctx, tx, "postgres")
	if sid != 4242 {
		t.Fatalf("sid on tx = %d, want 4242", sid)
	}
}

type oneConnDriver struct{}

func (oneConnDriver) Open(string) (driver.Conn, error) { return &oneConn{}, nil }

type oneConn struct {
	mu     sync.Mutex
	inTx   bool
	closed bool
}

func (c *oneConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *oneConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}
func (c *oneConn) Begin() (driver.Tx, error) { return c.BeginTx(context.Background(), driver.TxOptions{}) }
func (c *oneConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inTx = true
	return &oneTx{c: c}, nil
}
func (c *oneConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query != "SELECT pg_backend_pid()" {
		return nil, driver.ErrSkip
	}
	return &oneRows{vals: []driver.Value{int64(4242)}, done: false}, nil
}

type oneTx struct{ c *oneConn }

func (t *oneTx) Commit() error {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	t.c.inTx = false
	return nil
}
func (t *oneTx) Rollback() error {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	t.c.inTx = false
	return nil
}

type oneRows struct {
	vals []driver.Value
	done bool
}

func (r *oneRows) Columns() []string { return []string{"pg_backend_pid"} }
func (r *oneRows) Close() error      { return nil }
func (r *oneRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.vals)
	r.done = true
	return nil
}
