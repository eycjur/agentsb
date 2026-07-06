.PHONY: help install gateway-config run connect rm open upload build
.DEFAULT_GOAL := help

SANDBOX_NAME ?= $(subst _,-,$(shell basename $(CURDIR)))
WORKSPACE := /sandbox
PORT ?= 8000
MEMORY ?= 8Gi
GPU ?= 1
GPU_PCI_ID ?= $(shell lspci -D | awk 'BEGIN{IGNORECASE=1} /nvidia/ && /(VGA|3D|Display|3D controller)/ {print $$1; exit}')

install: gateway-config ## OpenShell インストール + MicroVM gateway 設定
	@command -v docker >/dev/null || { \
		echo "警告: docker がありません"; \
	}
	@command -v openshell >/dev/null || { \
		curl -LsSf https://raw.githubusercontent.com/NVIDIA/OpenShell/main/install.sh | sh; \
	}
	mkdir -p ~/.config/openshell
	ln -sf openshell/gateway.toml ~/.config/openshell/gateway.toml
	systemctl --user restart openshell-gateway
	openshell gateway list


	# @if [ -r /dev/kvm ]; then \
	# 	echo "KVM: OK (/dev/kvm)"; \
	# else \
	# 	echo "警告: /dev/kvm がありません。MicroVM には KVM が必要です"; \
	# fi
	# @if [ -d /sys/class/iommu_group ] && [ -n "$$(ls -A /sys/class/iommu_group 2>/dev/null)" ]; then \
	# 	echo "IOMMU: OK"; \
	# else \
	# 	echo "警告: IOMMU が無効の可能性があります (GPU パススルーに必要)"; \
	# fi

run: ## GPU 付き MicroVM サンドボックスを作成/接続
	@if ! openshell sandbox get "$(SANDBOX_NAME)" >/dev/null 2>&1; then \
		openshell sandbox create \
			--name "$(SANDBOX_NAME)" \
			--from ./Dockerfile \
			--gpu $(GPU) \
			--driver-config-json '{"vm":{"gpu_device_ids":["$(GPU_DEVICE)"]}}' \
			--memory $(MEMORY) \
			--upload .:$(WORKSPACE) \
			--forward $(PORT) \
			-- sleep infinity; \
	fi; \
	openshell sandbox connect "$(SANDBOX_NAME)"

rm: ## サンドボックスを削除
	openshell sandbox delete "$(SANDBOX_NAME)" 2>/dev/null || true

help: ## このヘルプを表示
	@grep -Eh '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
