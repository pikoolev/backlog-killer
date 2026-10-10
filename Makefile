.PHONY: generate check test build lint compose-up compose-down

generate:
	pnpm generate

check:
	pnpm check:generated
	go test -race ./...
	go vet ./...
	pnpm lint
	pnpm typecheck
	pnpm test
	pnpm build

test:
	go test -race ./...
	pnpm test

build:
	go build ./cmd/server
	pnpm build

lint:
	go vet ./...
	pnpm lint

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down
