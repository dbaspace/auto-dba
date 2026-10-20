# KeePlus / auto-dba v1.12.21

相对 [v1.12.20](changelog-v1.12.20.md)。

## CDC 同步任务

- 列表 description 瘦身：去掉 mappings 等大字段，写入 `list_table_counts`，首屏更轻；详情仍返回完整映射。
- 列表仅在编排中状态静默轮询；手动刷新有最短间隔；翻页 abort 在途 pipeline 探测。详情按页签懒加载 pipeline / Publication / 表映射；返回列表时关闭详情页签。
- 后台周期扫描 running/paused 初始化任务的表结构差异，写入内存缓存供列表展示。

## 密钥与配置

- 生产请固定独立的 `crypto_secret`，勿与 `jwt_secret` 同改。未配 crypto、仅改 JWT 热更时，进程自动把旧 FieldCrypto 并入 previous 环，降低「解密已存密码失败」。
- 启动时抽样校验实例密文可读；失败偏多打醒目日志。文档与 `config.example.yaml` 已补充轮换说明。

## 打包与发版

```bash
STAMP=v1.12.21 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.21
```

部署见 [deploy.md](deploy.md)。
