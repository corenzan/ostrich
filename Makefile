NAME=$(notdir $(CURDIR))
CGO_ENABLED=0
GOOS=linux

static: test
	@go build -a -ldflags '-extldflags "-static"' -o $(NAME) .
	
test: lint
	@go test ./...
	
lint: main.go
	@go fmt ./...
	@go vet ./...
	@go fix ./...

schema:
	@docker-compose exec -T database psql -h localhost -U postgres postgres < schema.sql

.PHONY: lint
