# feishu-plugin-platform — common dev & ops tasks.
# Local dev uses STORE=memory; production uses STORE=bitable (see PRODUCTION.md).

REGISTRY ?= your-registry
VERSION  ?= 0.1.0

.PHONY: help
help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "}{printf "  %-18s %s\n", $$1, $$2}'

.PHONY: test
test: ## vet + unit tests (incl. SDK enum reconciliation gate)
	go vet ./...
	go test ./...

.PHONY: sdk-enums
sdk-enums: ## refresh the basekit SDK enum golden from the installed/pinned SDK
	scripts/refresh-sdk-enums.sh

.PHONY: build
build: ## build all binaries to ./bin
	go build -o bin/api ./cmd/api
	go build -o bin/generator ./cmd/generator
	go build -o bin/bitable-bootstrap ./cmd/bitable-bootstrap

.PHONY: run-generator
run-generator: ## run the generator (DeepSeek by default; set DEEPSEEK_API_KEY)
	PORT=8090 go run ./cmd/generator

.PHONY: run-api
run-api: ## run the api (set GENERATOR_URL, STORE, PLATFORM_API_TOKEN as needed)
	PORT=8080 GENERATOR_URL=http://localhost:8090 WEB_DIR=./web go run ./cmd/api

.PHONY: bootstrap-bitable
bootstrap-bitable: ## create the Bitable Base+table (needs FEISHU_APP_ID/SECRET)
	go run ./cmd/bitable-bootstrap

.PHONY: up
up: ## docker compose up --build
	docker compose up --build

.PHONY: images
images: ## build api + generator images
	docker build -t $(REGISTRY)/feishu-plugin-platform/api:$(VERSION)       -f Dockerfile.api .
	docker build -t $(REGISTRY)/feishu-plugin-platform/generator:$(VERSION) -f Dockerfile.generator .

.PHONY: push
push: ## push images
	docker push $(REGISTRY)/feishu-plugin-platform/api:$(VERSION)
	docker push $(REGISTRY)/feishu-plugin-platform/generator:$(VERSION)

.PHONY: k8s-apply
k8s-apply: ## apply k8s manifests (edit deploy/k8s secrets/config first)
	kubectl apply -f deploy/k8s/

.PHONY: frontend-build
frontend-build: ## typecheck + build the Feishu container plugin
	cd frontend && npm install && npm run typecheck && npm run build
