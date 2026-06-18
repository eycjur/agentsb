# Makefile
.PHONY: install build up stop down update-makefile

DOCKER_HUB_USERNAME=eycjur
IMAGE_NAME=claude-sandbox
CONTAINER_NAME=$(subst _,-,$(shell basename $(CURDIR)))
UPSTREAM_MAKEFILE=https://raw.githubusercontent.com/eycjur/my-docker-sandbox/main/Makefile

install:
	brew trust --cask docker/tap/sbx@nightly
	brew install docker/tap/sbx
	sbx login

build:
	docker build \
		--no-cache \
		-t $(DOCKER_HUB_USERNAME)/$(IMAGE_NAME) \
		--push \
		.

up:
	@sbx ls -q | grep -qx '$(CONTAINER_NAME)' || \
		sbx create --name $(CONTAINER_NAME) \
			--template $(DOCKER_HUB_USERNAME)/$(IMAGE_NAME) \
			shell .
	sbx exec -it -w $(shell pwd) $(CONTAINER_NAME) zsh -l

stop:
	sbx stop $(CONTAINER_NAME)

down:
	sbx rm $(CONTAINER_NAME) --force

update-makefile:
	curl -fsSL -o Makefile $(UPSTREAM_MAKEFILE)
