DB_URL ?= postgres://dispatchr:dispatchr@localhost:15432/dispatchr?sslmode=disable
MIGRATE = docker compose run --rm migrate -path=/migrations -database="$(DB_URL)"

.PHONY: all build run test clean migrate-up migrate-down migrate-status migration-new

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
	docker compose run --rm migrate create -ext sql -dir /migrations "$(NAME)"
