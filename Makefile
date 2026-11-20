# Local development only. Production injects DISPATCHR_* configuration directly.
ENV_FILE ?= etc/config/config.local.env
COMPOSE = docker compose --env-file "$(ENV_FILE)"
MIGRATE = set -a; . "$(ENV_FILE)"; set +a; $(COMPOSE) run --rm migrate -path=/migrations -database="postgres://$$DISPATCHR_DB_USER:$$DISPATCHR_DB_PASSWORD@postgres:5432/$$DISPATCHR_DB_NAME?sslmode=$$DISPATCHR_DB_SSL_MODE"

.PHONY: all build run test clean migrate-up migrate-down migrate-status migration-new sql-format

all: build test

build:
	go build -o bin/dispatchr ./cmd/api

run:
	go run ./cmd/api

test:
	go test ./... -v

clean:
	rm -rf bin

migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

migrate-status:
	$(MIGRATE) version

migration-new:
	@test -n "$(NAME)" || (echo "Usage: make migration-new NAME=create_jobs"; exit 1)
	$(COMPOSE) run --rm migrate create -ext sql -dir /migrations "$(NAME)"

sql-format:
	pg_format -i migrations/*.sql
