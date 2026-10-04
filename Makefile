SHELL := /bin/bash
.DEFAULT_GOAL := help

export GOTOOLCHAIN := local

PKG      := github.com/abdulovia/cookcompass
BIN      := bin/cookcompass
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/server.Version=$(VERSION)
# Override to run CLI targets inside the container, e.g.
#   make approve-all RUN="docker compose exec -T app /app/cookcompass"
RUN      ?= ./$(BIN)
COVER_MIN ?= 70
SOURCE   ?= jsonld
COUNTRY  ?= all
# .env (from `make init`) sets PORT and the secrets docker compose needs.
-include .env
PORT     ?= 8080
IMAGE    ?= ghcr.io/abdulovia/cookcompass:latest


.PHONY: init doctor up-release help build run test test-short cover lint fmt fmt-check vet migrate seed \
        scrape-recipes scan recipe-create recipe-delete approve-all purge-pending \
        import-products export-products up down logs backup hooks ci docker-build clean

help: ## show targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n",$$1,$$2}'

build: ## compile the binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/cookcompass

run: build ## run the server locally on :8080
	$(BIN) serve

test: ## run all tests with race detector and coverage gate on internal/
	go test -race -count=1 -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out ./...
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	  echo "internal/ coverage: $$total% (min $(COVER_MIN)%)"; \
	  awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN{exit (t+0 < m+0)}' || { echo "coverage below $(COVER_MIN)%"; exit 1; }

test-short: ## fast tests (used by the pre-commit hook)
	go test -short -count=1 ./...

cover: test ## open HTML coverage report
	go tool cover -html=coverage.out -o coverage.html && echo "wrote coverage.html"

GOLANGCI_VERSION := v1.64.8
GOLANGCI := bin/golangci-lint

$(GOLANGCI):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)

lint: $(GOLANGCI) ## run golangci-lint (pinned version installed into bin/)
	$(GOLANGCI) run ./...

vet:
	go vet ./...

fmt: ## format code
	gofmt -s -w $$(git ls-files '*.go' 2>/dev/null || find . -name '*.go')

fmt-check:
	@out=$$(gofmt -s -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

migrate: build ## apply database migrations
	$(RUN) migrate

seed: build ## load the bundled seed dataset (idempotent)
	$(RUN) seed

scrape-recipes: build ## scrape recipes into the review queue (SOURCE=jsonld)
	$(RUN) scrape --source $(SOURCE)

import-recipes: build ## import a recipe dataset (FILE=x.json | URL=... [LIMIT=200] [APPROVE=1])
	$(RUN) recipes-import $(if $(FILE),--file $(FILE),) $(if $(URL),--url $(URL),) $(if $(LIMIT),--limit $(LIMIT),) $(if $(APPROVE),--approve)

scan: build ## scan product catalogs into the review queue (COUNTRY=all|US|...)
	$(RUN) scan --country $(COUNTRY)

recipe-create: build ## create a recipe from JSON (FILE=path.json)
	$(RUN) recipe create --file $(FILE)

recipe-delete: build ## soft-delete a recipe (ID=n)
	$(RUN) recipe delete --id $(ID)

approve-all: build ## approve every pending recipe and product
	$(RUN) approve-all

purge-pending: build ## delete every pending (unapproved) recipe and product
	$(RUN) purge-pending

import-products: build ## import products CSV (FILE=path.csv COUNTRY=US)
	$(RUN) products import --file $(FILE) --country $(COUNTRY)

export-products: build ## export products CSV to stdout (COUNTRY=US)
	@$(RUN) products export --country $(COUNTRY)

init: ## create .env with fresh secrets and the content/ folder (safe to re-run)
	@go run ./cmd/cookcompass init 2>/dev/null || \
	  docker run --rm --user "$$(id -u):$$(id -g)" -v "$(CURDIR)":/work -w /work $(IMAGE) init

# Targets that start containers create .env on first use (an explicit check,
# not a .env rule: that would make every `make` invocation run init).
ensure_env = @test -f .env || $(MAKE) -s init

define wait_healthy
	@for i in $$(seq 1 90); do \
	  if curl -fsS http://localhost:$(PORT)/healthz >/dev/null 2>&1; then \
	    echo "up: http://localhost:$(PORT)  ·  admin: http://localhost:$(PORT)/admin  ·  checks: make doctor"; exit 0; fi; sleep 1; \
	done; echo "app did not become healthy"; docker compose logs --tail=50 app; exit 1
endef

up: ## build from source and start the stack, wait until healthy
	$(ensure_env)
	VERSION=$(VERSION) docker compose up -d --build
	$(wait_healthy)

up-release: ## start the prebuilt image from ghcr.io (no Go, no build)
	$(ensure_env)
	docker compose pull app minio
	docker compose up -d --no-build
	$(wait_healthy)

doctor: ## check the installation and list what is left to do
	@if [ -n "$$(docker compose ps -q app 2>/dev/null)" ]; then \
	  docker compose exec -T app /app/cookcompass doctor; \
	else \
	  $(MAKE) -s build && $(RUN) doctor; \
	fi

down: ## stop the stack
	docker compose down

logs:
	docker compose logs -f app

backup: ## snapshot SQLite into backups/
	@mkdir -p backups; ts=$$(date +%Y%m%d-%H%M%S); \
	if [ -n "$$(docker compose ps -q app 2>/dev/null)" ]; then \
	  docker compose exec -T app /app/cookcompass backup --out /data/backups/cookcompass-$$ts.db && \
	  docker compose cp app:/data/backups/cookcompass-$$ts.db backups/cookcompass-$$ts.db; \
	else \
	  $(MAKE) -s build && $(RUN) backup --out backups/cookcompass-$$ts.db; \
	fi && echo "backup: backups/cookcompass-$$ts.db"

hooks: ## install git hooks from .githooks/
	git config core.hooksPath .githooks && chmod +x .githooks/* && echo "hooks installed"

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t cookcompass:ci .

ci: fmt-check vet lint test build docker-build ## everything the CI workflow runs

clean:
	rm -rf bin coverage.out coverage.html
