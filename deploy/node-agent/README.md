# StarRocks Node Agent

部署在 FE/BE 主机，提供 HTTP 白名单 API，供 auto-dba **集群部署 → StarRocks** 控制面编排预检/部署/启停。

## 构建（控制面机器）

```bash
./build.sh
# 产出 bin/node-agent，供控制面 SSH 下发读取
```

## 下发入口（推荐）

**平台运维 → 集群部署 → StarRocks → Node Agent Tab →「SSH 下发」**

- 选 host 类型实例、端口默认 **8091**、远程目录默认 `/home/sysadmin/auto-dba/node-agent`
- 成功后自动登记到 `/api/v1/sr-cluster/agents`（与配置中心平台 Agent :8090 **无关**）
- 依赖 `terminal.ssh_identity_file`

亦可本机手工运行后在 Tab「手工登记」：

```bash
./node-agent -listen :8091 -user agent -pass '***'
# 联调：-dry-run 跳过 mkfs/systemctl，装包走 stub
```

## 装包（OSS）

`POST /v1/sr/artifact/install` 接收控制面签发的 `download_url` + `sha256`，下载校验后解压到 `target_dir`（FE→`.../fe`，BE→`.../be`）。

其余 API 见控制面文档 [`docs/sr-cluster.md`](../../docs/sr-cluster.md) §3.3。页面入口：`/ops/cluster-deploy/starrocks`。
