.PHONY: bump-version lint run-caddy run-server vendor
MIDDLEWARES_LIST=caddy

bump-version:
	test $(from)
	test $(to)
	sed -i '' 's/version: $(from)/version: $(to)/' README.md
	for middleware in $(MIDDLEWARES_LIST) ; do \
		sed -i '' 's/github.com\/sc0rp10\/go-esi $(from)/github.com\/sc0rp10\/go-esi $(to)/' middleware/$$middleware/go.mod ; \
	done

lint: ## Run golangci-lint to ensure the code quality
	docker run --rm -v $(PWD):/app -w /app golangci/golangci-lint golangci-lint run

run-caddy: ## Build and run caddy binary
	cd middleware/caddy && $(MAKE) build && $(MAKE) run

run-server: ## Run server example
	go run middleware/server/main.go

run-standalone: ## Run standalone example
	go run middleware/standalone/main.go

vendor: ## Generate and prepare vendors for each plugin
	go mod tidy && go mod download
	for middleware in $(MIDDLEWARES_LIST) ; do \
	cd middleware/$$middleware && ($(MAKE) build || true) && cd -; done
