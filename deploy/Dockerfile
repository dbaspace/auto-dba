# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG APP=auto-dba
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/${APP} ./cmd/${APP}

FROM alpine:3.20
# SSH 下发 Backup Agent 使用 Go 内置 ssh/sftp（internal/sshx），无需 openssh-client
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ARG APP=auto-dba
COPY --from=build /out/${APP} /app/bin
COPY config/config.example.yaml /app/config/config.yaml
# 可选：将预构建的 linux Agent 制品打进镜像，供 backup.agent_artifact 使用
# COPY deploy/backup-agent/bin/backup-agent /app/deploy/backup-agent/bin/backup-agent
EXPOSE 8800 8801
ENTRYPOINT ["/app/bin"]
CMD ["start"]
