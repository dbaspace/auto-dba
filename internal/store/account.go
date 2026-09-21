package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

const accountCols = `id, instance_id, database_id, db_name, db_type, idc, username, password, permissions, description, ticket_id, created_by, updated_by, created_at, updated_at`

func nullStr(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}

func scanAccount(sc interface {
	Scan(dest ...any) error
}) (*DBAccount, error) {
	it := &DBAccount{}
	var perms, databaseID, ticketID, createdBy, updatedBy, desc sql.NullString
	if err := sc.Scan(
		&it.ID, &it.InstanceID, &databaseID, &it.DBName, &it.DBType, &it.IDC, &it.Username, &it.Password,
		&perms, &desc, &ticketID, &createdBy, &updatedBy, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.DatabaseID = nullStr(databaseID)
	it.TicketID = nullStr(ticketID)
	it.CreatedBy = nullStr(createdBy)
	it.UpdatedBy = nullStr(updatedBy)
	it.Description = nullStr(desc)
	it.Permissions = decodeStringSlice(nullStr(perms))
	it.PasswordSet = strings.TrimSpace(it.Password) != ""
	return it, nil
}

func (d *DB) ListAccounts(dbType, idc, instanceID, q string) ([]DBAccount, error) {
	query := `SELECT ` + accountCols + ` FROM tbl_db_accounts WHERE 1=1`
	args := []any{}
	if dbType != "" {
		query += ` AND db_type=?`
		args = append(args, dbType)
	}
	if idc != "" {
		query += ` AND idc=?`
		args = append(args, idc)
	}
	if instanceID != "" {
		query += ` AND instance_id=?`
		args = append(args, instanceID)
	}
	if q != "" {
		like := "%" + q + "%"
		query += ` AND (username LIKE ? OR db_name LIKE ? OR description LIKE ?)`
		args = append(args, like, like, like)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBAccount{}
	for rows.Next() {
		it, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetAccount(id string) (*DBAccount, error) {
	it, err := scanAccount(d.QueryRow(`SELECT `+accountCols+` FROM tbl_db_accounts WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) FindAccount(instanceID, username, dbName string) (*DBAccount, error) {
	it, err := scanAccount(d.QueryRow(
		`SELECT `+accountCols+` FROM tbl_db_accounts WHERE instance_id=? AND username=? AND db_name=?`,
		instanceID, username, dbName,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) ListAccountsByTicket(ticketID string) ([]DBAccount, error) {
	rows, err := d.Query(`SELECT `+accountCols+` FROM tbl_db_accounts WHERE ticket_id=?`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBAccount{}
	for rows.Next() {
		it, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) CreateAccount(it *DBAccount) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	if it.Permissions == nil {
		it.Permissions = []string{}
	}
	_, err := d.Exec(
		`INSERT INTO tbl_db_accounts(`+accountCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.InstanceID, it.DatabaseID, it.DBName, it.DBType, it.IDC, it.Username, it.Password,
		encodeJSON(it.Permissions), it.Description, it.TicketID, it.CreatedBy, it.UpdatedBy, it.CreatedAt, it.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateAccount(it *DBAccount) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if it.Permissions == nil {
		it.Permissions = []string{}
	}
	_, err := d.Exec(
		`UPDATE tbl_db_accounts SET instance_id=?, database_id=?, db_name=?, db_type=?, idc=?, username=?, password=?, permissions=?, description=?, ticket_id=?, updated_by=?, updated_at=? WHERE id=?`,
		it.InstanceID, it.DatabaseID, it.DBName, it.DBType, it.IDC, it.Username, it.Password,
		encodeJSON(it.Permissions), it.Description, it.TicketID, it.UpdatedBy, it.UpdatedAt, it.ID,
	)
	return err
}

func (d *DB) DeleteAccount(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_accounts WHERE id=?`, id)
	return err
}
