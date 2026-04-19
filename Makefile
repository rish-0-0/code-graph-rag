ROOT ?= .

# Detect OS for docker volume path quirks.
# Windows (Git Bash/MSYS) needs a leading `/` to stop path mangling.
# macOS/Linux use $(CURDIR) as-is.
ifeq ($(OS),Windows_NT)
	VOLUME := /$(CURDIR)/graph-out:/import
else
	VOLUME := $(pwd)/graph-out:/import
endif

.PHONY: build neo4j neo4j-import neo4j-stop build-darwin-arm64 build-linux-amd64 build-windows-amd64 test clean

NEO4J_CONTAINER := codegraph-neo4j

build:
	go run ./cmd/codegraph build --root $(ROOT) --output cypher --out-dir ./graph-out

neo4j:
	docker run --name $(NEO4J_CONTAINER) -p 7474:7474 -p 7687:7687 \
	-e NEO4J_AUTH=neo4j/test12345 -v "$(VOLUME)" -d neo4j:5

# Wait for the DB to come up, then stream graph.cypher into cypher-shell.
# Run after `make build` and `make neo4j`.
neo4j-import:
	@echo "waiting for neo4j to accept connections..."
	@docker exec $(NEO4J_CONTAINER) bash -c 'until cypher-shell -u neo4j -p test12345 "RETURN 1" >/dev/null 2>&1; do sleep 2; done'
	docker exec -i $(NEO4J_CONTAINER) cypher-shell -u neo4j -p test12345 --file /import/graph.cypher

neo4j-stop:
	-docker stop $(NEO4J_CONTAINER)
	-docker rm $(NEO4J_CONTAINER)

# Cross-compilation targets
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o bin/codegraph-darwin-arm64 ./cmd/codegraph

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o bin/codegraph-linux-amd64 ./cmd/codegraph

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o bin/codegraph-windows-amd64.exe ./cmd/codegraph

test:
	go test ./...

clean:
	rm -rf bin graph-out .codegraph
