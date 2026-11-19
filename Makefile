.PHONY: build test

build:
	go build -trimpath -o bin/kubekaze ./cmd/kubekaze

test:
	go vet ./...
	go test ./...
