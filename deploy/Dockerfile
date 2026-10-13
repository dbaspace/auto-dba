# syntax=docker/dockerfile:1
# 一体镜像：Go API + 前端静态资源（http.serve_static: true）
# 构建：docker build -f deploy/Dockerfile -t auto-dba:latest .
# 或：./scripts/pack_release.sh docker

# ── 前端 ───────────────────────────────────────────────
FROM node:20-alpine AS frontend
WORKDIR /src/frontend
RUN corepack enable && corepack prepare pnpm@10.32.0 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# ── 后端 ───────────────────────────────────────────────
FROM golang:1.24-alpine AS backend
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG APP=auto-dba
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/${APP} ./cmd/${APP}

# ── 运行 ───────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ARG APP=auto-dba
COPY --from=backend /out/${APP} /app/bin/auto-dba
COPY --from=frontend /src/frontend/dist /app/frontend/dist
# 主机 Web 终端：默认 scripts_dir / static_dir 相对 /app
COPY scripts/terminal /app/scripts/terminal
COPY assets/terminal /app/assets/terminal
RUN chmod a+x /app/scripts/terminal/*.sh
# 默认 serve_static: true；生产请挂载真实 config 或走 Nacos
COPY deploy/config.docker.example.yaml /app/config/config.yaml
EXPOSE 8800 8801
ENTRYPOINT ["/app/bin/auto-dba"]
CMD ["-config", "config/config.yaml", "start"]
