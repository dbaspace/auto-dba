# KeePlus / auto-dba v1.12.59

相对 [v1.12.58](changelog-v1.12.58.md)。

## DQL 下载流程展示

- 详情「下载流程」改为单列纵向步骤（一步一行、↓ 连接），第一步标「起」。
- 步骤文案白话化：选择导出方式、查询数据并生成文件；远程另有下发到就近执行器、传回平台临时目录。
- 展示执行方式：本机直连，或执行器名称与地址（`exec_progress.executor_url`）。

## 打包与发版

```bash
STAMP=v1.12.59 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.59
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
