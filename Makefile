.PHONY: run test testv fmt

run:
	go run ./cmd/slashbot

test:
	go test ./...

testv:
	go test -v ./...

fmt:
	gofmt -w .
