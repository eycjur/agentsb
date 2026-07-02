# Makefile
.PHONY: help install build run stop rm open update-makefile
.DEFAULT_GOAL := help

CONTAINER_NAME ?= $(subst _,-,$(shell basename $(CURDIR)))
WORKSPACE := /home/agent/workspace
PORT ?= 8000
MEMORY ?= 8g
PLATFORM ?= linux/amd64
UPSTREAM_MAKEFILE := https://raw.githubusercontent.com/eycjur/my-docker-sandbox/main/Makefile

# yolo モード向け: 権限を落とし、新しい特権取得を禁止
DOCKER_SECURITY_FLAGS := \
	--init \
	--cap-drop=ALL \
	--security-opt=no-new-privileges \
	--pids-limit=512 \
	--memory=$(MEMORY) \
	--env IS_SANDBOX=1

install: ## Docker と NVIDIA Container Toolkit の確認
	@command -v docker >/dev/null || { \
		echo "docker が必要です: https://docs.docker.com/engine/install/"; \
		exit 1; \
	}
	@docker info >/dev/null 2>&1 || { \
		echo "Docker daemon が起動していません"; \
		exit 1; \
	}
	@if command -v nvidia-smi >/dev/null 2>&1; then \
		nvidia-smi >/dev/null 2>&1 || echo "警告: nvidia-smi が失敗しました"; \
	else \
		echo "警告: nvidia-smi が見つかりません"; \
	fi
	@docker run --rm --gpus all nvidia/cuda:12.6.3-base-ubuntu24.04 nvidia-smi >/dev/null 2>&1 \
		|| echo "警告: コンテナから GPU を使えません。nvidia-container-toolkit をインストールしてください"

build: ## CI: イメージをビルドして push
	docker build \
		--no-cache \
		.

run: install ## コンテナを作成/起動して zsh に入る
	@if docker inspect "$(CONTAINER_NAME)" >/dev/null 2>&1; then \
		docker start "$(CONTAINER_NAME)" 2>/dev/null || true; \
	else \
		docker run -d \
			--name "$(CONTAINER_NAME)" \
			$(DOCKER_SECURITY_FLAGS) \
			--gpus all \
			-p $(PORT):$(PORT) \
			-v "$$(pwd):$(WORKSPACE)" \
			-w "$(WORKSPACE)" \
			--user agent \
			"$(CONTAINER_NAME)" sleep infinity; \
	fi
	docker exec -it -u agent -w "$(WORKSPACE)" "$(CONTAINER_NAME)" zsh -l

stop: ## コンテナを停止
	docker stop "$(CONTAINER_NAME)"

rm: ## コンテナを削除
	docker rm -f "$(CONTAINER_NAME)" 2>/dev/null || true

update-makefile: ## 最新の Makefile を取得して更新
	curl -fsSL -o Makefile $(UPSTREAM_MAKEFILE)

help: ## このヘルプを表示
	@grep -Eh '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
