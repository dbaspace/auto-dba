package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const UserRegistrationPending = "pending"

// UserRegistration 自助注册待审批申请。通过或驳回后删除，不长期保留密码哈希。
type UserRegistration struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	PasswordHash string `json:"-"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

func (d *DB) CreateUserRegistration(username, displayName, passwordHash string) (*UserRegistration, error) {
	rec := &UserRegistration{
		ID:           uuid.NewString(),
		Username:     strings.TrimSpace(username),
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: passwordHash,
		Status:       UserRegistrationPending,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if rec.DisplayName == "" {
		rec.DisplayName = rec.Username
	}
	_, err := d.Exec(
		`INSERT INTO tbl_user_registrations(id, username, display_name, password_hash, status, created_at) VALUES(?,?,?,?,?,?)`,
		rec.ID, rec.Username, rec.DisplayName, rec.PasswordHash, rec.Status, rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (d *DB) GetPendingUserRegistrationByUsername(username string) (*UserRegistration, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	rec, err := scanUserRegistration(d.QueryRow(
		`SELECT id, username, display_name, password_hash, status, created_at
		 FROM tbl_user_registrations WHERE LOWER(username)=LOWER(?) AND status=? ORDER BY created_at LIMIT 1`,
		username, UserRegistrationPending,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

func (d *DB) ListPendingUserRegistrations() ([]UserRegistration, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name, password_hash, status, created_at
		 FROM tbl_user_registrations WHERE status=? ORDER BY created_at`,
		UserRegistrationPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRegistration
	for rows.Next() {
		rec, err := scanUserRegistration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (d *DB) GetUserRegistration(id string) (*UserRegistration, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	rec, err := scanUserRegistration(d.QueryRow(
		`SELECT id, username, display_name, password_hash, status, created_at
		 FROM tbl_user_registrations WHERE id=?`,
		id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

func (d *DB) DeleteUserRegistration(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_user_registrations WHERE id=?`, strings.TrimSpace(id))
	return err
}

type userRegistrationScanner interface {
	Scan(dest ...any) error
}

func scanUserRegistration(sc userRegistrationScanner) (*UserRegistration, error) {
	rec := &UserRegistration{}
	err := sc.Scan(&rec.ID, &rec.Username, &rec.DisplayName, &rec.PasswordHash, &rec.Status, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return rec, nil
}
