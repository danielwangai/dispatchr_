# Local development only. Production injects DISPATCHR_* configuration directly.
ENV_FILE ?= etc/config/config.local.env
COMPOSE = docker compose --env-file "$(ENV_FILE)"

define migrate-command
set -a; . "$(ENV_FILE)"; set +a; $(COMPOSE) run --rm migrate -path=/migrations -database="postgres://$$DISPATCHR_DB_USER:$$DISPATCHR_DB_PASSWORD@postgres:5432/$(1)?sslmode=$$DISPATCHR_DB_SSL_MODE"
endef

MIGRATE = $(call migrate-command,$$DISPATCHR_DB_NAME)
MIGRATE_TEST = $(call migrate-command,$$DISPATCHR_TEST_DB_NAME)

.PHONY: all build run test clean fmt migrate-up migrate-down migrate-status migration-new test-db-create test-db-migrate integration-test sql-format

all: build test

build:
	go build -o bin/dispatchr ./cmd/api

run:
	go run ./cmd/api -v

# unit test
unit-test:
	go test ./... -v

# integration test
integration-test: test-db-migrate
	@set -a; . "$(ENV_FILE)"; set +a; \
		DISPATCHR_TEST_DATABASE_URL="postgres://$$DISPATCHR_DB_USER:$$DISPATCHR_DB_PASSWORD@localhost:$$DISPATCHR_DB_PORT/$$DISPATCHR_TEST_DB_NAME?sslmode=$$DISPATCHR_DB_SSL_MODE" \
		go test -count=1 -v -tags=integration ./internal/storage/postgres

go-fmt:
	gofmt -w $$(rg --files -g '*.go')

sql-fmt:
	pg_format -i migrations/*.sql

fmt:
	make go-fmt && make sql-fmt

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

test-db-create:
	$(COMPOSE) up -d postgres
	@set -a; . "$(ENV_FILE)"; set +a; \
		$(COMPOSE) exec -T postgres psql -U "$$DISPATCHR_DB_USER" -d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '$$DISPATCHR_TEST_DB_NAME'" | grep -q 1 || \
		$(COMPOSE) exec -T postgres psql -U "$$DISPATCHR_DB_USER" -d postgres -c "CREATE DATABASE \"$$DISPATCHR_TEST_DB_NAME\""

test-db-migrate: test-db-create
	$(MIGRATE_TEST) up
