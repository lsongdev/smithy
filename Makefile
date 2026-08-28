BINARY_NAME=bin/smithy

.PHONY: build clean test

build:
	mkdir -p $(dir $(BINARY_NAME))
	GOARCH=arm64 GOOS=darwin go build -ldflags="-s -w" -o ${BINARY_NAME}-darwin-arm64
	GOARCH=amd64 GOOS=darwin go build -ldflags="-s -w" -o ${BINARY_NAME}-darwin-amd64
	GOARCH=amd64 GOOS=linux go build -ldflags="-s -w" -o ${BINARY_NAME}-linux
	GOARCH=amd64 GOOS=windows go build -ldflags="-s -w" -o ${BINARY_NAME}-windows-amd64.exe

test:
	go test ./...

clean:
	go clean
	rm -f ${BINARY_NAME}-darwin-arm64 ${BINARY_NAME}-darwin-amd64 ${BINARY_NAME}-linux ${BINARY_NAME}-windows-amd64.exe
