.PHONY: build run

build: ## Build caddy binary
	go mod tidy
	go mod download
	xcaddy build --with github.com/sc0rp10/go-esi@latest=../.. --with github.com/sc0rp10/go-esi/middleware/caddy=./

run: ## Run caddy with go-esi
	./caddy run