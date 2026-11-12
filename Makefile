.PHONY: run test mutation build db-reset migrate openapi backfill-voice

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

# Mutation testing de internal/service con gremlins (ver docs/mutation-testing).
# Los tests de los servicios están en test/, así que corre toda la suite por
# cada mutante (--integration): tarda unos 45 minutos. No corre en el CI.
# Sin DATABASE_HOST ni RABBITMQ_URL (que llegan del .env): los tests de
# integración hacen DROP TABLE y correrían cientos de veces contra esa base.
mutation:
	go clean -testcache
	env -u DATABASE_HOST -u RABBITMQ_URL gremlins unleash ./internal/service --integration --coverpkg ./internal/service/... \
		--timeout-coefficient 5 --workers 6 \
		--invert-assignments --invert-logical --invert-loopctrl --remove-self-assignments \
		--invert-bitwise --invert-bwassign --output docs/mutation-testing/after.json

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

# Publica el snapshot actual de autorizacion solo en la cola de una replica de
# voice. Usar ARGS=--dry-run para contar sin publicar.
backfill-voice:
	go run ./cmd/backfillvoice $(ARGS)
