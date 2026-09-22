.DEFAULT_GOAL := help
SHELL := /bin/bash

COMPOSE   := docker compose
BACKEND   := backend
FRONTEND  := frontend
DB_VOLUME := kahan-hai_kahan-hai-data

# Local (non-docker) probe defaults
Q   ?= coca cola 1l
P   ?= all
LAT ?= 28.6315
LON ?= 77.2167

##@ Getting started

.PHONY: up
up: ## Build and start everything (the one command to run after cloning)
	$(COMPOSE) up --build -d
	@echo ""
	@echo "  web  → http://localhost:$${WEB_PORT:-3000}"
	@echo "  api  → http://localhost:$${API_PORT:-8080}/api/health"
	@echo ""
	@echo "  follow logs with: make logs"

.PHONY: down
down: ## Stop everything (price history in the volume is kept)
	$(COMPOSE) down

.PHONY: restart
restart: down up ## Restart the whole stack

##@ Day to day

.PHONY: logs
logs: ## Tail logs from all services
	$(COMPOSE) logs -f

.PHONY: logs-api
logs-api: ## Tail logs from the Go API only
	$(COMPOSE) logs -f api

.PHONY: ps
ps: ## Show service status
	$(COMPOSE) ps

.PHONY: build
build: ## Rebuild images without starting
	$(COMPOSE) build

.PHONY: rebuild
rebuild: ## Rebuild from scratch, ignoring layer cache
	$(COMPOSE) build --no-cache

.PHONY: shell-api
shell-api: ## Open a shell in the API container
	$(COMPOSE) exec api sh

.PHONY: health
health: ## Check the API is up
	@curl -fsS http://localhost:$${API_PORT:-8080}/api/health | sed 's/,/,\n/g' || echo "API not reachable"

.PHONY: search
search: ## Query the running API, e.g. make search Q="amul milk"
	@curl -fsS "http://localhost:$${API_PORT:-8080}/api/search?q=$$(printf %s "$(Q)" | jq -sRr @uri)" | jq '.results[] | {platform, n: (.products|length), error, tookMs}'

##@ Local development (no docker)

.PHONY: dev-api
dev-api: ## Run the API on the host against a local Chrome
	cd $(BACKEND) && mkdir -p data && go run ./cmd/server

.PHONY: dev-web
dev-web: ## Run the Next.js dev server on the host
	cd $(FRONTEND) && npm run dev

.PHONY: install-web
install-web: ## Install frontend dependencies
	cd $(FRONTEND) && npm install

##@ Adapters

.PHONY: probe
probe: ## Run one adapter directly: make probe P=blinkit Q="amul milk"
	cd $(BACKEND) && go run ./cmd/probe -platform "$(P)" -q "$(Q)" -lat $(LAT) -lon $(LON)

.PHONY: probe-headful
probe-headful: ## Same as probe but shows the browser window (for debugging)
	cd $(BACKEND) && go run ./cmd/probe -platform "$(P)" -q "$(Q)" -lat $(LAT) -lon $(LON) -headful

# When an adapter parses nothing, the question is always "what did the platform
# actually send?". This writes every intercepted response to ./dump so it can be
# read, diffed against a working capture, or turned into a test fixture.
.PHONY: probe-dump
probe-dump: ## Probe and save every intercepted JSON payload to ./dump
	@rm -rf dump && mkdir -p dump
	cd $(BACKEND) && KH_DUMP_DIR=$(CURDIR)/dump go run ./cmd/probe -platform "$(P)" -q "$(Q)" -lat $(LAT) -lon $(LON)
	@echo; ls -la dump

##@ Code quality

.PHONY: test
test: ## Run Go tests
	cd $(BACKEND) && go test ./...

.PHONY: fmt
fmt: ## Format Go code
	cd $(BACKEND) && go fmt ./...

.PHONY: vet
vet: ## Run go vet
	cd $(BACKEND) && go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod
	cd $(BACKEND) && go mod tidy

.PHONY: check
check: fmt vet test ## fmt + vet + test

##@ Database

.PHONY: db-shell
db-shell: ## Open a sqlite shell against the container's database
	$(COMPOSE) exec api sh -c 'command -v sqlite3 >/dev/null || { echo "sqlite3 not in image; use make db-copy instead"; exit 1; }; sqlite3 /data/kahan-hai.db'

# The database runs in WAL mode, so recent writes live in the -wal sidecar. A
# plain `docker cp` of the .db alone silently returns stale (often empty) data.
define copy_db
	docker cp kahan-hai-api:/data/kahan-hai.db $(1) >/dev/null 2>&1 && \
	docker cp kahan-hai-api:/data/kahan-hai.db-wal $(1)-wal >/dev/null 2>&1 || true; \
	docker cp kahan-hai-api:/data/kahan-hai.db-shm $(1)-shm >/dev/null 2>&1 || true
endef

.PHONY: db-copy
db-copy: ## Copy the database (with its WAL) out of the volume to ./kahan-hai.db
	@$(call copy_db,./kahan-hai.db)
	@echo "copied to ./kahan-hai.db (plus -wal/-shm)"

.PHONY: db-stats
db-stats: ## Show how many price observations have been recorded
	@$(call copy_db,/tmp/kahan-hai-stats.db)
	@sqlite3 /tmp/kahan-hai-stats.db \
		'SELECT platform, COUNT(*) AS observations, MAX(observed_at) AS latest FROM observations GROUP BY platform;' \
		2>/dev/null || echo "no data yet: run a search first (needs sqlite3 on the host)"

.PHONY: db-reset
db-reset: ## DESTRUCTIVE: delete all recorded price history
	@read -p "Delete all price history? [y/N] " ok && [ "$$ok" = "y" ] || exit 1
	$(COMPOSE) down
	docker volume rm $(DB_VOLUME) || true
	@echo "volume removed; next 'make up' starts fresh"

##@ Cleanup

.PHONY: clean
clean: ## Remove build artifacts and node_modules
	rm -rf $(FRONTEND)/.next $(FRONTEND)/node_modules $(BACKEND)/data
	cd $(BACKEND) && go clean

.PHONY: nuke
nuke: ## DESTRUCTIVE: remove containers, images and volumes for this project
	@read -p "Remove all containers, images and volumes? [y/N] " ok && [ "$$ok" = "y" ] || exit 1
	$(COMPOSE) down -v --rmi local

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nkahan-hai\n\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""
