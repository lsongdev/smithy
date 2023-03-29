BINARY_NAME=bin/smithy

build:
	GOARCH=arm64 GOOS=darwin go build -ldflags="-s -w" -o ${BINARY_NAME}-darwin
	GOARCH=amd64 GOOS=linux go build -ldflags="-s -w" -o ${BINARY_NAME}-linux
	GOARCH=amd64 GOOS=windows go build -ldflags="-s -w" -o ${BINARY_NAME}-amd64.exe

clean:
	go clean
	rm ${BINARY_NAME}-darwin
	rm ${BINARY_NAME}-linux
	rm ${BINARY_NAME}-x64.exe
