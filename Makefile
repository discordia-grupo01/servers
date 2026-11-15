.PHONY: run test mutation build db-reset migrate openapi backfill-voice contract-deps-up contract-deps-down contract-test

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
# voice. Usar ARGS=--dry-run para contar sin publicar. Para reconciliación
# periódica: ARGS="--interval 5m".
backfill-voice:
	go run ./cmd/backfillvoice $(ARGS)

# Tests de contrato: Schemathesis contra el servicio levantado con un Postgres y
# un RabbitMQ efimeros y locales (puertos 55505 y 55515). Ver README.
contract-deps-up:
	docker run -d --rm --name contract-servers-pg -p 55505:5432 -e POSTGRES_USER=discordia -e POSTGRES_PASSWORD=discordia -e POSTGRES_DB=discordia_servers_contract postgres:16-alpine
	docker run -d --rm --name contract-servers-rabbitmq -p 55515:5672 rabbitmq:3.13-alpine
	until docker exec contract-servers-pg pg_isready -U discordia -d discordia_servers_contract >/dev/null 2>&1; do sleep 1; done
	sleep 2
	MSYS_NO_PATHCONV=1 docker run --rm -v $(shell pwd)/migrations:/migrations --network container:contract-servers-pg migrate/migrate -path=/migrations -database "postgres://discordia:discordia@localhost:5432/discordia_servers_contract?sslmode=disable" up

contract-deps-down:
	-docker rm -f contract-servers-pg contract-servers-rabbitmq

contract-test:
	bash scripts/contract-test.sh
