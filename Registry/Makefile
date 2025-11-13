### Application commands

help:
	@echo "Available commands:"
	@echo "  make install-deps   - Install project dependencies"
	@echo "  make build          - Build the application"
	@echo "  make run            - Run the application"
	@echo "  make proto-gen      - Generate protobuf code"

install-deps: .env
	go mod tidy

build: .env
	go build -o bin/app cmd/main.go

run: .env
	go run cmd/main.go

proto-gen:
	protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative proto/maps.proto

.PHONY: install-deps build run proto-gen