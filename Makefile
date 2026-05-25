SHELL := /bin/bash

.PHONY: db-up db-down db-logs migrate-up migrate-down migrate-version build

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-logs:
	docker compose logs -f postgres

migrate-up:
	DATABASE_DSN=$${DATABASE_DSN:-postgres://poe2:poe2@127.0.0.1:5432/poe2?sslmode=disable} go run ./cmd/migrate -cmd up

migrate-down:
	DATABASE_DSN=$${DATABASE_DSN:-postgres://poe2:poe2@127.0.0.1:5432/poe2?sslmode=disable} go run ./cmd/migrate -cmd down -n 1

migrate-version:
	DATABASE_DSN=$${DATABASE_DSN:-postgres://poe2:poe2@127.0.0.1:5432/poe2?sslmode=disable} go run ./cmd/migrate -cmd version

build:
	go build ./...
