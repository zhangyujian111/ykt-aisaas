GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/go)
export GOPROXY := https://goproxy.cn,direct

BIN     := bin/aisaas
PKG     := ./...
MIGRATE_DSN ?= mysql://root:root@tcp(127.0.0.1:13306)/ykt_aisaas

.PHONY: build run test vet tidy clean docker-up docker-down mock seed-e2e

build:
	$(GO) build -o $(BIN) ./cmd/aisaas

run: build
	./$(BIN) -config config.yaml

test:
	$(GO) test $(PKG) -count=1

vet:
	$(GO) vet $(PKG)

tidy:
	$(GO) mod tidy

# 依赖栈（端口错开宿主已有 MySQL:3306）：MySQL 13306 / Redis 16379
docker-up:
	docker compose -f deploy/docker-compose-deps.yml up -d
	@echo "wait mysql ready..." && sleep 8

docker-down:
	docker compose -f deploy/docker-compose-deps.yml down

# 依赖的 migrate 工具（首次自动下载）
migrate:
	@command -v migrate >/dev/null 2>&1 || $(GO) install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate -path migrations -database "$(MIGRATE_DSN)" up

mock:
	$(GO) run ./tools/mockupstream -port 18080 &

seed-e2e:
	$(GO) run ./tools/seedmodel -model demo-chat -base-url http://127.0.0.1:18080/v1 -upstream demo-chat -api-key demo-key

clean:
	rm -rf bin
