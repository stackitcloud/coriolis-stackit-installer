.PHONY: build test check

build:
	go build -trimpath -o bin/coriolis-stackit ./cmd/coriolis-stackit

test:
	go test ./...

check: test build
