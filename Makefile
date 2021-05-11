NAME=app
CGO_ENABLED=0
GOOS=linux

build: test
	@go build -o dist/$(NAME) .
	
test: lint
	@go test ./...
	
lint: main.go
	@go fmt ./...
	@go vet ./...
	@go fix ./...

.PHONY: lint
