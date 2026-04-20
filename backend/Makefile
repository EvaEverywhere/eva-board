.PHONY: build test run migrate

build:
	go build ./...

test:
	go test ./...

run:
	go run ./cmd/server

migrate:
	go run ./cmd/migrate up
