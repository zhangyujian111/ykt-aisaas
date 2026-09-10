# ykt-aisaas — AI SaaS 服务（多租户、计量计费、RAG）
#
# 纯 Go 构建：sqlite 仅出现在测试文件中且用的是纯 Go 实现（glebarez），
# pgx / cilium-ebpf 也都不需要 CGO，因此可以完全静态编译。

# 注意：go.mod 声明 go 1.25.0，基础镜像不能低于该版本，否则构建直接失败。
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/aisaas \
      ./cmd/aisaas

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=build /out/aisaas /app/aisaas
# 内置默认配置作为兜底；生产环境挂载 /app/config.yaml 覆盖。
COPY config.yaml /app/config.yaml
# migrations 一并打包，便于用同一个镜像执行数据库迁移。
COPY migrations/ /app/migrations/

EXPOSE 8190

# -config 是相对路径，依赖 WORKDIR=/app。
ENTRYPOINT ["/app/aisaas", "-config", "config.yaml"]
