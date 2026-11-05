package cdc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// publicationSlotPlan 不连库时就能确定的语句序列。
type publicationSlotPlan struct {
	Statements      []string
	Added           []string
	ReplicaIdentity []string
}

func publicationCreateSQL(pub, schema string, tables []string) string {
	return fmt.Sprintf(`CREATE PUBLICATION %s FOR TABLE %s`, quotePGIdent(pub), strings.Join(qualifyPGTables(schema, tables), ", "))
}

func publicationAddSQL(pub, schema string, tables []string) string {
	return fmt.Sprintf(`ALTER PUBLICATION %s ADD TABLE %s`, quotePGIdent(pub), strings.Join(qualifyPGTables(schema, tables), ", "))
}

func logicalSlotCreateSQL(slot string) string {
	return fmt.Sprintf(`SELECT pg_create_logical_replication_slot(%s, 'pgoutput')`, quotePGLiteral(slot))
}

func publicationAllTablesError(pub string) error {
	return fmt.Errorf("publication %s 为整库发布(FOR ALL TABLES)，请先执行 DROP PUBLICATION %s 后重新初始化", pub, pub)
}

// planPublicationSlotSQL 初始化先 CREATE PUBLICATION 和创建 slot，再补缺表。
// 加表只 ALTER PUBLICATION ADD TABLE。整库发布在初始化路径直接失败。
func planPublicationSlotSQL(addOnly, pubExists, allTables bool, pub, slot, schema string, tables, missing []string) (publicationSlotPlan, error) {
	var plan publicationSlotPlan
	if addOnly {
		if len(missing) == 0 {
			return plan, nil
		}
		plan.Statements = []string{publicationAddSQL(pub, schema, missing)}
		plan.Added = append([]string(nil), missing...)
		plan.ReplicaIdentity = append([]string(nil), missing...)
		return plan, nil
	}
	if pubExists && allTables {
		return plan, publicationAllTablesError(pub)
	}
	if !pubExists {
		plan.Statements = append(plan.Statements, publicationCreateSQL(pub, schema, tables))
		plan.ReplicaIdentity = append(plan.ReplicaIdentity, tables...)
	}
	if strings.TrimSpace(slot) != "" {
		plan.Statements = append(plan.Statements, logicalSlotCreateSQL(slot))
	}
	if pubExists && len(missing) > 0 {
		plan.Statements = append(plan.Statements, publicationAddSQL(pub, schema, missing))
		plan.Added = append([]string(nil), missing...)
		plan.ReplicaIdentity = append(plan.ReplicaIdentity, missing...)
	}
	return plan, nil
}

func isCreatePublication(stmt string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE PUBLICATION")
}

func lookupPublication(ctx context.Context, db *sql.DB, pub string) (exists, allTables bool, err error) {
	if db == nil {
		return false, false, fmt.Errorf("db nil")
	}
	var all bool
	err = db.QueryRowContext(ctx, `SELECT puballtables FROM pg_publication WHERE pubname=$1`, pub).Scan(&all)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("查询 publication 失败: %w", err)
	}
	return true, all, nil
}
