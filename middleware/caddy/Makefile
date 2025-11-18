.PHONY: build run

build: ## Build caddy binary
	cd ../.. && go mod tidy
	xcaddy build --with github.com/sc0rp10/go-esi=../..

run: ## Run caddy with go-esi
	./caddy run