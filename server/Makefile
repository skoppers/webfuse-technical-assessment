SQLC_VERSION ?= v1.31.1
TEST_DATABASE_URL ?= postgres://localhost:5432/saa_test?sslmode=disable

.PHONY: sqlc test test-db

# Regenerate internal/store/gen/ from queries.sql and the migrations.
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate -f internal/store/sqlc.yaml

test:
	go test ./...

# Store, session, reaper, ingest, webhook and API integration tests against a throwaway database.
test-db:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./internal/store/... ./internal/session/... ./internal/reaper/... ./internal/ingest/... ./internal/webhook/... ./internal/api/...
