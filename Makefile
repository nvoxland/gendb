.PHONY: build clean test lint run docs docs-serve docs-docker

BINARY=gendb
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/gendb

clean:
	rm -rf $(BUILD_DIR)

test:
	go test ./...

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY)

.DEFAULT_GOAL := build
