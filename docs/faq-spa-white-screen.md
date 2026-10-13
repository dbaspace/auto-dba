# FAQ：生产一体部署白屏

面向 **KeePlus / auto-dba** 一体包（`http.serve_static: true`）或经域名（如 `keeplus.restosuite.cn`）访问时出现白屏的排查资料。打包方式见 [deploy.md](deploy.md)。

## 现象怎么读

| 你看到的 | 更可能的含义 |
|----------|----------------|
| 页面空白，控制台有 `Failed to load module script` / MIME 不是 JS | 拿到了 **HTML** 当 JS（旧 SPA 回落或缓存了旧入口） |
| Network 里 `GET /` → **304**，随后 `/assets/*.js` 失败 | 浏览器用了**缓存的旧 `index.html`**，里面的 Vite hash 已失效 |
| `/assets/index-xxxx.js` → **404**，响应体 `{"error_msg":"404 Route Not Found"}` | **APISIX（或同类网关）未匹配路由**，请求没到 auto-dba |
| `/assets/...` → **404**，响应几乎为空或纯文本 | 已到后端，但磁盘上**没有该 hash 文件**（包不全 / cwd 不对） |
| 启动日志无 `boot: serve_static` | 当前进程 **`serve_static` 未开启** |
| 浏览器标签页是红底白 S（或其它非 KeePlus 图标），侧栏蓝标正常 | **标签 favicon** 加载失败（`/favicon.svg` / `/favicon.ico` 500/404），浏览器回退到同域缓存；与侧栏 logo 无关 |

> 本仓库后端静态 404 **不会**返回 `error_msg` 字段。出现 `{"error_msg":"404 Route Not Found"}` 时，优先查 **入口网关**，不要先怀疑前端打包 hash。

---

## Q1：重新打包部署后仍白屏，JS/CSS 404，且是 `404 Route Not Found`

### 结论

域名走 **APISIX（:80）** 时，常只配了 `/api/*` 或个别路径；`/assets/*` **没有 Route**，网关直接 404，页面拿不到 JS/CSS → 白屏。

典型 Network：

- 文档入口（域名根）能打开（绿/200）
- `http://你的域名/assets/index-XXXX.js` → **404**
- Preview：`{"error_msg":"404 Route Not Found"}`
- Remote Address 往往是网关公网 IP:**80**，不是后端 **8800**

### 处理

1. 在 APISIX（或前置 Nginx）为该 Host 增加转发，至少覆盖：
   - `/assets/*` → auto-dba upstream（如 `127.0.0.1:8800` 或内网服务）
   - 前端路由与入口：`/`、以及无扩展名的 SPA 路径（或统一 `/*` 到同一 upstream）
   - `/favicon.svg`、`/favicon.ico`、`/apple-touch-icon.png` → 同一 upstream（浏览器会自动请求根路径图标）
   - `/api/*` → 同一 upstream（或既有 API 路由）
2. 后端保持一体部署：`http.serve_static: true`，在**发布包根目录**启动。
3. 验证：

```bash
# 经域名：修好后应为 200，Content-Type 含 javascript
curl -sI 'http://keeplus.restosuite.cn/assets/index-XXXX.js'

# 直连后端（绕过网关）：若这里 200、域名仍 404 → 只改网关即可，不必再打包
curl -sI 'http://<后端内网IP>:8800/assets/index-XXXX.js'

# 标签页图标（与白屏同源网关问题时常一起坏）
curl -sI 'http://keeplus.restosuite.cn/favicon.svg'
curl -sI 'http://keeplus.restosuite.cn/favicon.ico'
# 期望 200；修通后请对该域硬刷新或清站点数据，否则旧红标可能仍占 Tab
```

hash 以当前包内 `frontend/dist/index.html` 里引用的文件名为准。

---

## Q2：日志里大量 `GET /` → 304，强制刷新前一直白屏

### 结论

浏览器缓存了**旧的 `index.html`**，仍引用上一版 Vite 产物路径（如 `/assets/index-旧hash.js`）。重新发版后旧文件已不存在。

旧版 `serveSPA` 还会把「找不到的 `.js`」回落成 `index.html`，浏览器按 ES module 解析 HTML → 白屏。新版（含 SPA 硬化）对带扩展名的静态资源缺失返回 **真实 404**，不再回落。

### 处理

1. **立即**：Ctrl/Cmd+Shift+R 强刷，或清站点缓存。
2. 确认已部署含硬化逻辑的二进制；`index.html` / `/` 响应应带 `Cache-Control: no-cache`（或等价），降低再 304 到陈旧入口的概率。
3. `/assets/*` 带 content hash，可长缓存；**不要**对 `index.html` 做长时间 CDN/浏览器强缓存。

```bash
curl -sI http://host:8800/ | grep -i cache-control
# 期望含 no-cache

curl -sI http://host:8800/assets/不存在的旧hash.js
# 期望 404，且 Body 不是整页 HTML
```

---

## Q3：`serve_static: true` 配了，启动后仍像没静态、或 index 找不到

### 结论

1. **工作目录**：一体包必须在包根启动，相对路径 `frontend/dist` 才解析得对。
2. **Nacos**：远端 DataId 若写了 `http:` 段但**未写** `serve_static`，旧逻辑可能把本地 `true` 冲成 `false`；现已保留本地值。若远端**显式** `serve_static: false`，则以远端为准。
3. 启动日志应有：

```text
boot: serve_static root=/绝对路径/frontend/dist index=ok
```

若出现 `index.html missing`：cwd 或包布局错误（缺 `frontend/dist`）。

### 处理

```bash
cd /opt/auto-dba   # 包根：含 bin/、frontend/dist/
./bin/auto-dba -config config/config.yaml
```

Nacos / 本地示例：

```yaml
http:
  host: "0.0.0.0"
  port: 8800
  serve_static: true
```

---

## Q4：标签页图标不是 KeePlus（红底白 S 等），侧栏蓝标却正常

### 结论

侧栏 logo 来自打包进前端的资源；**浏览器 Tab 图标**只认站点根路径的 `/favicon.svg`、`/favicon.ico`（以及 `apple-touch-icon`）。域名上这两个若 **500 / 网关 404 Route Not Found**，浏览器会回退到同域历史缓存（例如曾打开过 StarRocks 控制台 `/starrocks-webroot/...`，或其它 restosuite 子站图标），于是出现「标题仍是 KeePlus、图标却不对」。

### 处理

1. 确认发布包 `frontend/dist/` 含 `favicon.svg`、`favicon.ico`、`apple-touch-icon.png`（`index.html` 已声明）。
2. 直连后端与经域名均应为 **200**：

```bash
curl -sI http://127.0.0.1:8800/favicon.svg
curl -sI http://127.0.0.1:8800/favicon.ico
curl -sI http://keeplus.restosuite.cn/favicon.svg
curl -sI http://keeplus.restosuite.cn/favicon.ico
```

3. 直连 200、域名失败 → 与 Q1 相同，给 APISIX 补 favicon 路径转发（或统一 `/*`）。
4. 修通后对该 Host **硬刷新 / 清站点数据**，否则旧图标缓存仍会显示。

---

## Q5：一体包、分服务包分别怎么用，避免再踩白屏

| 模式 | 产物 | 页面从哪来 | 注意 |
|------|------|------------|------|
| **一体** `pack_release.sh all` | `auto-dba-release-*-*.tgz` | 后端 `serve_static` 同端口 | 网关必须转发 `/`、`/assets/*`、`/favicon.*`；包根启动 |
| **仅后端** `backend` | `auto-dba-backend-*-*.tgz` | Vite 或 Nginx，**不是** 8800 页 | `serve_static: false` |
| **仅前端** `frontend` | `auto-dba-frontend-*.tgz` | Nginx `root` 指 `dist/` | Nginx 对 `index.html` 禁长缓存；`/api/` 反代后端 |

发版示例：`STAMP=v1.5.0 ./scripts/pack_release.sh all`（及 `backend` / `frontend`）。说明见 [deploy.md](deploy.md)。

---

## Q6：如何快速判断卡在哪一层

按顺序执行：

```bash
# 1) 直连后端静态（应 200）
curl -sI "http://127.0.0.1:8800/assets/$(basename $(ls frontend/dist/assets/index-*.js | head -1))"

# 2) 经域名同路径
curl -sI "http://你的域名/assets/同上文件名"
```

| 直连 | 域名 | 结论 |
|------|------|------|
| 200 | 200 | 正常；若仍白屏 → 清缓存 / 看控制台其它错误 |
| 200 | `error_msg: 404 Route Not Found` | **改 APISIX/网关路由** |
| 404 | 404 | 查包内是否有该文件、cwd、`serve_static`、启动日志 |
| 连接失败 | — | 进程未起或未监听 8800 |

浏览器：打开 DevTools → Network → 点开失败的 JS → 看 Status、Response、Remote Address（80 多为网关，8800 多为直连后端）。

---

## 相关改动与文档

- 静态托管硬化：`internal/router/router.go`（`serveSPA`）、启动日志：`internal/server/server.go`
- Nacos 保留本地 `serve_static`：`internal/config/nacos.go`
- 打包与部署总览：[deploy.md](deploy.md)
- 前端构建分离背景：[frontend-deploy.md](frontend-deploy.md)
- Nacos：[nacos-config.md](nacos-config.md)
