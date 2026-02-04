.PHONY: run test testv fmt build deploy

-include .env
export

run:
	go run ./cmd/slashbot

test:
	go test ./...

testv:
	go test -v ./...

fmt:
	gofmt -w .

build:
	GOOS=linux GOARCH=amd64 go build -o slashbot-linux ./cmd/slashbot

deploy: build
	@test -n "$(DEPLOY_USER)" || (echo "DEPLOY_USER is not set. Add it to .env" && exit 1)
	@test -n "$(DEPLOY_HOST)" || (echo "DEPLOY_HOST is not set. Add it to .env" && exit 1)
	@test -n "$(DEPLOY_PATH)" || (echo "DEPLOY_PATH is not set. Add it to .env" && exit 1)
	scp slashbot-linux $(DEPLOY_USER)@$(DEPLOY_HOST):$(DEPLOY_PATH)/slashbot.new
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'mv $(DEPLOY_PATH)/slashbot.new $(DEPLOY_PATH)/slashbot'
