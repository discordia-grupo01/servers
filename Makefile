.PHONY: run test build db-reset migrate

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
	go test ./... -coverpkg=./internal/... -cover

build:
	go build -o bin/api ./cmd/api

db-reset:
	docker compose down -v
	docker compose up -d db

# Aplica migrations/*.sql en orden contra la BDD que indique .env (local o
# Supabase). Sin tracking de "ya aplicadas" (mismo criterio que el
# docker-entrypoint-initdb.d de docker-compose): pensado para correr una vez
# contra un schema vacio.
migrate:
	@for f in migrations/*.sql; do \
		echo "Aplicando $$f..."; \
		{ [ -n "$(DATABASE_SCHEMA)" ] && echo "SET search_path TO $(DATABASE_SCHEMA);"; cat $$f; } | \
			docker run --rm -i --net=host postgres:16-alpine \
			psql "postgres://$(DATABASE_USER):$(DATABASE_PASSWORD)@$(DATABASE_HOST):$(DATABASE_PORT)/$(DATABASE_NAME)?sslmode=$(DATABASE_SSLMODE)" \
			-v ON_ERROR_STOP=1 || exit 1; \
	done
