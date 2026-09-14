SQLC_VERSION ?= v1.31.1
TEST_DATABASE_URL ?= postgres://localhost:5432/saa_test?sslmode=disable
IMAGE ?= saa-server

.PHONY: sqlc test test-db run lint docker-build docker-run

# Regenerate internal/store/gen/ from queries.sql and the migrations.
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate -f internal/store/sqlc.yaml

test:
	go test ./...

# Store, session, reaper, ingest, webhook and API integration tests against a throwaway database.
# -p 1: every package truncates (and the store package re-creates) the same tables, so they cannot run in parallel.
test-db:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -p 1 ./internal/store/... ./internal/session/... ./internal/reaper/... ./internal/ingest/... ./internal/webhook/... ./internal/api/...

# Run the server from source, loading .env into the environment when present.
run:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; go run ./cmd/server

# go vet, plus staticcheck when it is installed.
lint:
	go vet ./...
	@command -v staticcheck >/dev/null && staticcheck ./... || echo "staticcheck not installed, skipped"

# Build the production image; the build context is the repo root (parent dir).
docker-build:
	docker build -f Dockerfile -t $(IMAGE) ..

# Run the built image with the local .env, serving on localhost:8080.
docker-run:
	docker run --rm --env-file .env -p 8080:8080 $(IMAGE)
