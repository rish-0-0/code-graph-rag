ROOT          ?= .
ENDPOINT      ?= http://localhost:8088
EMBED_DIR      = embeddings
COMPOSE        = docker compose -f $(EMBED_DIR)/docker-compose.yml --env-file $(EMBED_DIR)/.env

.PHONY: help \
        build install test clean \
        build-darwin-arm64 build-linux-amd64 build-windows-amd64 \
        stack-up stack-down stack-restart stack-logs stack-status stack-clean \
        index embed refresh search ui open-ui open-neo4j \
        all

help:
	@echo "codegraph — top-level targets"
	@echo ""
	@echo "  Go CLI:"
	@echo "    make build              # go build -o codegraph"
	@echo "    make install            # go install ./cmd/codegraph"
	@echo "    make test               # go test ./..."
	@echo "    make build-{darwin-arm64,linux-amd64,windows-amd64}"
	@echo ""
	@echo "  Embeddings + UI + Neo4j stack (docker compose in ./embeddings/):"
	@echo "    make stack-up           # build + start pgvector, ollama, neo4j, indexer"
	@echo "    make stack-down         # stop containers, keep volumes"
	@echo "    make stack-clean        # stop + remove volumes (wipes pg/neo4j data)"
	@echo "    make stack-status       # docker compose ps"
	@echo "    make stack-logs         # tail indexer logs"
	@echo ""
	@echo "  End-to-end indexing pipeline:"
	@echo "    make index              # codegraph build → .codegraph/graph.jsonl"
	@echo "    make embed              # ship docs/snippets to pgvector via $(ENDPOINT)"
	@echo "    make refresh            # reload graph.jsonl into Neo4j"
	@echo "    make all                # stack-up + index + embed + refresh"
	@echo ""
	@echo "  Quick links (after the stack is up):"
	@echo "    make open-ui            # http://localhost:8088"
	@echo "    make open-neo4j         # http://localhost:7474"

# ----- Go CLI ---------------------------------------------------------------

build:
	go build -o codegraph ./cmd/codegraph

install:
	go install ./cmd/codegraph

test:
	go test ./...

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o bin/codegraph-darwin-arm64 ./cmd/codegraph

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o bin/codegraph-linux-amd64 ./cmd/codegraph

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o bin/codegraph-windows-amd64.exe ./cmd/codegraph

clean:
	rm -rf bin graph-out .codegraph codegraph codegraph.exe

# ----- Embeddings / UI / Neo4j stack ---------------------------------------

# Ensure .env exists so docker compose won't complain.
$(EMBED_DIR)/.env:
	@if [ ! -f "$(EMBED_DIR)/.env" ]; then \
		cp $(EMBED_DIR)/.env.example $(EMBED_DIR)/.env; \
		echo "created $(EMBED_DIR)/.env from .env.example"; \
	fi

stack-up: $(EMBED_DIR)/.env
	$(COMPOSE) up -d --build

stack-down:
	$(COMPOSE) down

stack-restart:
	$(COMPOSE) restart indexer

stack-status:
	$(COMPOSE) ps

stack-logs:
	$(COMPOSE) logs -f --tail=200 indexer

stack-clean:
	$(COMPOSE) down -v

# ----- Indexing pipeline ----------------------------------------------------

# 1) parse Go → .codegraph/graph.jsonl (read by Neo4j-loader on refresh).
index:
	go run ./cmd/codegraph build --root $(ROOT)

# 2) push docs+source snippets to pgvector via the indexer service.
embed:
	EMBEDDINGS_API_ENDPOINT=$(ENDPOINT) go run ./cmd/codegraph embed

# 3) tell the indexer service to reload graph.jsonl into Neo4j.
refresh:
	@curl -fsS -X POST $(ENDPOINT)/graph/refresh \
		| python -c "import sys,json; d=json.load(sys.stdin); print('reload:', d)" \
		2>/dev/null || curl -fsS -X POST $(ENDPOINT)/graph/refresh

# Convenience: semantic CLI search.
search:
	@if [ -z "$(Q)" ]; then echo "usage: make search Q='your query'"; exit 2; fi
	EMBEDDINGS_API_ENDPOINT=$(ENDPOINT) go run ./cmd/codegraph search $(Q)

# Open the URLs in the default browser (Windows: start, macOS: open, Linux: xdg-open).
ui open-ui:
	@cmd.exe /c start $(ENDPOINT) 2>/dev/null || open $(ENDPOINT) 2>/dev/null || xdg-open $(ENDPOINT)

open-neo4j:
	@cmd.exe /c start http://localhost:7474 2>/dev/null || open http://localhost:7474 2>/dev/null || xdg-open http://localhost:7474

# Full pipeline: stack up, build the graph, embed everything, load Neo4j.
all: stack-up index embed refresh
	@echo ""
	@echo "Stack is up. Open $(ENDPOINT)/ to start exploring."
