PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto')
CORE_DIR := core-service

.PHONY: proto proto-py swagger build test vet run up down

proto:
	protoc -I $(PROTO_DIR) \
		--go_out=$(CORE_DIR)/internal/pb --go_opt=paths=source_relative \
		--go-grpc_out=$(CORE_DIR)/internal/pb --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

proto-py:
	python3 -m grpc_tools.protoc -I $(PROTO_DIR) \
		--python_out=checkin-service/app/pb \
		--grpc_python_out=checkin-service/app/pb \
		$(PROTO_FILES)

# swag version is pinned by core-service/go.mod
swagger:
	cd $(CORE_DIR) && go tool swag init -g cmd/core/main.go -o docs --parseInternal

build:
	cd $(CORE_DIR) && go build -o bin/core ./cmd/core

test:
	cd $(CORE_DIR) && go test ./...

vet:
	cd $(CORE_DIR) && go vet ./...

run:
	cd $(CORE_DIR) && go run ./cmd/core

up:
	docker compose up -d --build

down:
	docker compose down
