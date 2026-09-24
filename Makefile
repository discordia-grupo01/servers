.PHONY: run test build db-reset migrate openapi

-include .env
export

DATABASE_HOST ?= localhost
DATABASE_PORT ?= 5432
DATABASE_NAME ?= discordia_servicio
DATABASE_USER ?= discordia
DATABASE_PASSWORD ?= discordia
DATABASE_SSLMODE ?= disable

run:
	go run ./cmd/api

test:
	go test ./... -coverpkg=./internal/... -cover -p 1

build:
	go build -o bin/api ./cmd/api

db-reset:
	docker compose down -v
	docker compose up -d db
	
migrate:
	docker run --rm -v $(shell pwd)/migrations:/migrations --net=host migrate/migrate \
		-path=/migrations \
		-database "postgres://$(DATABASE_USER):$(DATABASE_PASSWORD)@$(DATABASE_HOST):$(DATABASE_PORT)/$(DATABASE_NAME)?sslmode=$(DATABASE_SSLMODE)$(if $(DATABASE_SCHEMA),&search_path=$(DATABASE_SCHEMA),)" \
		up

openapi:
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g cmd/api/main.go -o ./.openapi-tmp --parseInternal -q
	npx --yes swagger2openapi@7 ./.openapi-tmp/swagger.yaml -o openapi.yaml --patch
	rm -rf ./.openapi-tmp
