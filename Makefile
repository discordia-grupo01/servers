.PHONY: run test build db-reset

run:
	go run ./cmd/api

test:
	go test ./... -coverpkg=./internal/... -cover

build:
	go build -o bin/api ./cmd/api

db-reset:
	docker compose down -v
	docker compose up -d db
