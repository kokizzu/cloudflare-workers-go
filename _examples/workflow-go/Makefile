.PHONY: dev
dev:
	wrangler dev

.PHONY: build
build:
	go run ../../cmd/workers-assets-gen -mode=go -workflows=MyWorkflow
	GOOS=js GOARCH=wasm go build -o ./build/app.wasm ./...

.PHONY: deploy
deploy:
	wrangler deploy
