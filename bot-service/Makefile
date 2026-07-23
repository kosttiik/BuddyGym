PROTO_DIR := ../proto

.PHONY: proto-py lint format test test-integration migrate run dry-run

proto-py:
	uv run python -m grpc_tools.protoc -I $(PROTO_DIR) --python_out=src/pb --grpc_python_out=src/pb $(PROTO_DIR)/buddygym/v1/checkin.proto

lint:
	uv run ruff check .
	uv run ruff format --check .

format:
	uv run ruff format .

test:
	uv run pytest

test-integration:
	uv run pytest -m integration

migrate:
	uv run alembic upgrade head

run:
	uv run python -m src.main

dry-run:
	BOT_MODE=dry-run uv run python -m src.main
