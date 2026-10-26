# KeePlus / auto-dba v1.12.44

相对 [v1.12.43](changelog-v1.12.43.md)。无表结构变更。

## SQL 执行器下发

- 下发不再靠 `go.mod` 找二进制。安装包没有源码，旧逻辑会走到文件系统根，找不到包内的 `deploy/sql-agent/bin/sql-agent`，再去执行 `go build` 并报 `go.mod file not found`。
- 现在按当前工作目录、程序所在目录的上一级，以及源码树查找已构建的 `sql-agent`。找到就直接下发，日志写明所用路径。只有源码树里没有这份二进制时才编译。

## 打包与发版

```bash
STAMP=v1.12.44 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.44
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
