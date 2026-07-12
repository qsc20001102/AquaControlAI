.PHONY: dev build test lint
dev:
	go run ./cmd/server
build:
	go build ./...
	cd web && pnpm build
test:
	go test ./...
	cd web && pnpm test
lint:
	go vet ./...
	cd web && pnpm lint
