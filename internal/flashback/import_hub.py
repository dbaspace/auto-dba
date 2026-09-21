#!/usr/bin/env python3
"""Copy Hub flashback kernel files and rewrite imports for auto-dba."""
import re
from pathlib import Path

SRC = Path("/Users/chenjiachi/go/src/universe_saturn/jupiter-db_manage_hub/internal/service")
DST = Path("/Users/chenjiachi/go/src/auto-dba/internal/flashback")

FILES = [
    "flashback_walparse.go",
    "flashback_wal.go",
    "flashback_datum.go",
    "flashback_fpw.go",
    "flashback_xact.go",
    "flashback_toast.go",
    "flashback_dict.go",
    "flashback_dict_snap.go",
    "flashback_catalog.go",
    "flashback_ddl.go",
    "flashback_sql.go",
    "flashback_version.go",
    "flashback_mysql.go",
    "flashback_mysql_dump.go",
    "flashback_mysql_dict_snap.go",
    "flashback_cloud.go",
    "flashback_cloud_wal.go",
    "flashback_cloud_tencent.go",
    "flashback_cloud_unpack.go",
    "flashback_cloud_run.go",
    "flashback_precheck.go",
    "flashback_run.go",
]

# Drop Hub connect from copied precheck; we provide it in target.go
DROP_FUNCS = {
    "flashback_precheck.go": ["flashbackConnectTarget"],
    "flashback_mysql.go": ["connectSourceMySQL", "flashbackResolveMySQLCreds"],
}


def drop_funcs(src: str, names: list[str]) -> str:
    lines = src.splitlines(keepends=True)
    out = []
    i = 0
    while i < len(lines):
        m = re.match(r"func (\w+)\(", lines[i])
        if m and m.group(1) in names:
            brace = 0
            started = False
            while i < len(lines):
                brace += lines[i].count("{") - lines[i].count("}")
                if "{" in lines[i]:
                    started = True
                i += 1
                if started and brace <= 0:
                    break
            out.append("\n")
            continue
        out.append(lines[i])
        i += 1
    return "".join(out)


def rewrite_imports(src: str) -> str:
    # strip hub / mdm / zap / ginplus imports
    src = re.sub(r'\tmdmmodel "git\.restosuite\.cn/universe/go-sdk/service/mdm/model"\n', "", src)
    src = re.sub(r'\t"git\.restosuite\.cn/universe/jupiter-db_manage_hub/internal/service/dto"\n', "", src)
    src = re.sub(r'\t"git\.restosuite\.cn/universe/jupiter-db_manage_hub/internal/storage/flashback"\n', "", src)
    src = re.sub(r'\t"git\.restosuite\.cn/universe/jupiter-db_manage_hub/internal/storage/databases/ent"\n', "", src)
    src = re.sub(r'\t"git\.restosuite\.cn/universe/jupiter-db_manage_hub/pkg/utils/log"\n', "", src)
    src = re.sub(r'\t"go\.uber\.org/zap"\n', "", src)
    src = re.sub(r'\t"github.com/gin-gonic/gin"\n', "", src)
    # collapse empty import () leftovers later via gofmt
    return src


def rewrite_types(src: str) -> str:
    reps = [
        ("package service", "package flashback"),
        ("mdmmodel.ResourceDbsInfo", "ResourceDbsInfo"),
        ("mdmmodel.MySQLDBTypeEnum", '"mysql"'),
        ("mdmmodel.PostgreSQLDBTypeEnum", '"postgres"'),
        ("*ent.DomainInstance", "*DomainInstance"),
        ("ent.DomainInstance", "DomainInstance"),
        ("dto.FlashbackTaskReq", "FlashbackTaskReq"),
        ("dto.FlashbackPrecheckResult", "FlashbackPrecheckResult"),
        ("dto.FlashbackCheckItem", "FlashbackCheckItem"),
        ("dto.FlashbackTask", "FlashbackTaskDTO"),
        ("flashback.TaskRow", "TaskRow"),
        ("flashback.SQLRow", "SQLRow"),
        ("flashback.LogRow", "LogRow"),
        ("flashback.StatusPending", "StatusPending"),
        ("flashback.StatusRunning", "StatusRunning"),
        ("flashback.StatusSucceeded", "StatusSucceeded"),
        ("flashback.StatusFailed", "StatusFailed"),
        ("flashback.StatusCancelled", "StatusCancelled"),
        ("flashback.KindUndo", "KindUndo"),
        ("flashback.KindRedo", "KindRedo"),
        ("flashback.OutputFlashback", "OutputFlashback"),
        ("flashback.OutputOriginal", "OutputOriginal"),
        ("flashback.NewID()", "NewID()"),
        ("zap.Error(err)", "err"),
        ("zap.String(", "/*zap*/("),
        ("zap.Int64(", "/*zap*/("),
        ("zap.Int(", "/*zap*/("),
        ("zap.Any(", "/*zap*/("),
        ("log.Warn(", "fbLog("),
        ("log.Info(", "fbLog("),
        ("log.Error(", "fbLog("),
        ("*gin.Context", "context.Context"),
        ("c.Request.Context()", "c"),
    ]
    for a, b in reps:
        src = src.replace(a, b)
    return src


def main():
    DST.mkdir(parents=True, exist_ok=True)
    for name in FILES:
        raw = (SRC / name).read_text()
        if name in DROP_FUNCS:
            raw = drop_funcs(raw, DROP_FUNCS[name])
        raw = rewrite_imports(raw)
        raw = rewrite_types(raw)
        (DST / name).write_text(raw)
        print("wrote", name, "lines", raw.count("\n") + 1)


if __name__ == "__main__":
    main()
