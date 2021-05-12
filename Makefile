CGO_ENABLED=0
GOOS=linux

run: test
	@go run .

build: test
	@go build .
	
test: lint
	@go test ./...
	
lint: main.go
	@go fmt ./...
	@go vet ./...
	@go fix ./...

.PHONY: lint test
