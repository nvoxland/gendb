.PHONY: build clean test lint run docs docs-serve docs-docker

BINARY=autodb
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/autodb

clean:
	rm -rf $(BUILD_DIR)

test:
	go test ./...

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY)

.DEFAULT_GOAL := build
