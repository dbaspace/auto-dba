package store

import (
	"strings"
	"testing"
)

func TestNormalizeMigrationEnvs(t *testing.T) {
	same := &MigrationTask{Env: "test"}
	NormalizeMigrationEnvs(same)
	if same.SourceEnv != "test" || same.TargetEnv != "test" || same.Env != "test" {
		t.Fatalf("same env: %+v", same)
	}

	cross := &MigrationTask{SourceEnv: "test", TargetEnv: "ai-beijing"}
	NormalizeMigrationEnvs(cross)
	if cross.SourceEnv != "test" || cross.TargetEnv != "ai-beijing" || cross.Env != "test" {
		t.Fatalf("cross env: %+v", cross)
	}

	kept := &MigrationTask{Env: "prod", SourceEnv: "test", TargetEnv: "ai-beijing"}
	NormalizeMigrationEnvs(kept)
	if kept.Env != "test" || kept.SourceEnv != "test" || kept.TargetEnv != "ai-beijing" {
		t.Fatalf("env follows source: %+v", kept)
	}
}

func TestMigTaskColsIncludeEnvs(t *testing.T) {
	n := 0
	for _, p := range strings.Split(migTaskCols, ",") {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	if n != 39 {
		t.Fatalf("migration task columns = %d, want 39", n)
	}
	if !strings.Contains(migTaskCols, "source_env") || !strings.Contains(migTaskCols, "target_env") {
		t.Fatal("source_env/target_env missing from column list")
	}
}
