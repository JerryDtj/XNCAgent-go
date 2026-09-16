.PHONY: up down run

up:
	docker compose -f deploy/docker-compose.yaml up -d

down:
	docker compose -f deploy/docker-compose.yaml down

run:
	go run ./cmd/gateway
