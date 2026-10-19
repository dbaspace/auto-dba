# KeePlus / auto-dba v1.12.18

相对 [v1.12.17](changelog-v1.12.17.md)。已有库启动时为同步槽补 `active_pid`、`confirmed_flush_lsn`、`lag_bytes` 列，无需单独执行 SQL。

## 同步与迁移

- 同步列表从 PostgreSQL 复制槽目录采集 pid、restart LSN、confirmed flush LSN 和滞后字节。手动采集显示进度；可按状态、日志积压、数据库域名、任务名筛选。创建或重新初始化 CDC 任务只登记槽名，不再把已采集的 LSN 和滞后清掉。
- CDC 同步任务增加状态筛选。运行列表按编排中、运行中、已暂停、异常、失败、重新初始化中过滤（链路不健康算异常）；待审核只有待审批；审批结果按已通过、已驳回、已撤销。三个页签的选项不混用。
- 任务详情在实例对不上时，仍展示任务里记下的源端、目标端主机和端口。表映射保留已保存的 Kafka topic 与 Routine Load 名称、状态。

## 配置与用户

- 连接器可按环境、Kafka 地址、Debezium 地址筛选。
- 用户管理可按用户名搜索。LDAP 显示名去掉紧贴在汉字前的工号前缀，列表展示同样处理。

## 打包与发版

```bash
STAMP=v1.12.18 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.18
```

部署见 [deploy.md](deploy.md)。
