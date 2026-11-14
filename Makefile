# ---------------------------------------------------------------------------
# base-admin-core 常用命令
#
# Windows 下如果没有 make，可以直接参考每个目标中的命令手动执行；
# 本地开发脚本见 .dev/env.ps1（把 Go / pnpm 缓存放在仓库内，避免污染全局环境）。
# ---------------------------------------------------------------------------

SHELL := /usr/bin/env bash
BACKEND_DIR := backend
FRONTEND_DIR := frontend
APP_NAME := base-admin
BIN_DIR := $(BACKEND_DIR)/bin

.DEFAULT_GOAL := help

.PHONY: help
help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ------------------------------- 后端 ---------------------------------------

.PHONY: backend-run
backend-run: ## 启动后端服务（读取 backend/.env）
	cd $(BACKEND_DIR) && go run ./cmd/server

.PHONY: backend-migrate
backend-migrate: ## 执行数据库迁移
	cd $(BACKEND_DIR) && go run ./cmd/migrate

.PHONY: backend-build
backend-build: ## 编译后端二进制到 backend/bin
	cd $(BACKEND_DIR) && go build -trimpath -ldflags "-s -w" -o bin/server ./cmd/server

.PHONY: backend-test
backend-test: ## 运行后端单元测试
	cd $(BACKEND_DIR) && go test ./...

.PHONY: backend-vet
backend-vet: ## 运行 go vet 静态检查
	cd $(BACKEND_DIR) && go vet ./...

.PHONY: backend-tidy
backend-tidy: ## 整理 go.mod / go.sum
	cd $(BACKEND_DIR) && go mod tidy

.PHONY: backend-fmt
backend-fmt: ## 格式化 Go 代码
	cd $(BACKEND_DIR) && gofmt -w .

.PHONY: backend-check
backend-check: backend-fmt backend-vet backend-test ## 格式 + 静态检查 + 测试

# ------------------------------- 前端 ---------------------------------------

.PHONY: frontend-install
frontend-install: ## 安装前端依赖
	cd $(FRONTEND_DIR) && pnpm install

.PHONY: frontend-dev
frontend-dev: ## 启动前端开发服务器（默认 5888 端口）
	cd $(FRONTEND_DIR) && pnpm dev

.PHONY: frontend-build
frontend-build: ## 构建前端产物
	cd $(FRONTEND_DIR) && pnpm build

.PHONY: frontend-typecheck
frontend-typecheck: ## 前端类型检查
	cd $(FRONTEND_DIR) && pnpm check:type

.PHONY: frontend-lint
frontend-lint: ## 前端 lint
	cd $(FRONTEND_DIR) && pnpm lint

# ------------------------------- 组合 ---------------------------------------

.PHONY: check
check: backend-check frontend-typecheck ## 后端 + 前端全部检查

.PHONY: dev
dev: ## 提示本地同时启动前后端
	@echo "请打开两个终端："
	@echo "  1) make backend-run    # http://127.0.0.1:8080"
	@echo "  2) make frontend-dev   # http://127.0.0.1:5888"
