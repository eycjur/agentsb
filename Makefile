# Makefile
.PHONY: up down exec

up:
	docker compose build
	docker compose up -d

down:
	docker compose down

exec:
	docker compose exec -it sandbox zsh

