.PHONY: help dev up down destroy logs ps migrate seed k3d-up k3d-down build test fmt vet token

CLUSTER ?= vibedeploy
REGISTRY_HOST_PORT ?= 5001
BASE_DOMAIN ?= lab.localhost
K3D_API_PORT ?= 6443

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

dev: k3d-up up token ## One-shot local dev: k3d cluster + compose stack + print token
	@echo ""
	@echo "  Admin UI : http://localhost:5173"
	@echo "  API      : http://localhost:8080"
	@echo "  MinIO    : http://localhost:9001  (vibedeploy / vibedeploypw)"
	@echo "  Registry : http://localhost:5001"
	@echo ""
	@echo "  Export the token printed above as VIBEDEPLOY_TOKEN to use the Skill."

up: ## Bring up the dev stack (PG, MinIO, Redis, Registry, Control Plane, Build Service, Web)
	@test -f .env || cp .env.example .env
	@mkdir -p kube && touch kube/.gitkeep
	docker compose up -d --build
	@echo "Waiting for control-plane to be healthy..."
	@for i in $$(seq 1 60); do \
	  curl -fsS http://localhost:8080/healthz >/dev/null 2>&1 && echo "control-plane OK" && exit 0; \
	  sleep 1; \
	done; echo "control-plane not healthy"; docker compose logs --tail=40 control-plane; exit 1

token: ## Print the bootstrap deploy token (from control-plane logs on first boot)
	@docker compose logs control-plane 2>/dev/null \
	  | awk '/BOOTSTRAP DEPLOY TOKEN/{getline; gsub(/^[[:space:]]+|[[:space:]]+$$/,""); print; exit}' \
	  || true

down: ## Stop the dev stack
	docker compose down

logs: ## Tail control-plane + build-service logs
	docker compose logs -f control-plane build-service

ps: ## docker compose ps
	docker compose ps

migrate: ## Run DB migrations against running PG
	docker compose exec -T postgres psql -U vibedeploy -d vibedeploy -f /docker-entrypoint-initdb.d/0001_init.up.sql || \
	  cat migrations/0001_init.up.sql | docker compose exec -T postgres psql -U vibedeploy -d vibedeploy

seed: ## Create a dev deploy-token and print it
	@docker compose exec -T control-plane /control-plane seed-dev-token

k3d-up: ## Create a local k3d cluster, expose API on host:6443, write kube/config for control-plane
	@which k3d >/dev/null || (echo "install k3d: https://k3d.io"; exit 1)
	@if k3d cluster list | awk 'NR>1{print $$1}' | grep -qx $(CLUSTER); then \
	  echo "k3d cluster $(CLUSTER) already exists"; \
	else \
	  k3d cluster create $(CLUSTER) \
	    --servers 1 --agents 2 \
	    --api-port "$(K3D_API_PORT)" \
	    --port "80:80@loadbalancer" \
	    --port "443:443@loadbalancer" \
	    --registry-config deploy/k3d/registries.yaml \
	    --k3s-arg "--disable=servicelb@server:*"; \
	fi
	@mkdir -p kube
	@k3d kubeconfig get $(CLUSTER) \
	  | sed -E "s|server: https://[^:]+:[0-9]+|server: https://host.docker.internal:$(K3D_API_PORT)|" \
	  > kube/config
	@chmod 600 kube/config
	@echo "k3d up; kube/config written. Restart control-plane to pick it up: docker compose restart control-plane"

k3d-down: ## Delete the local k3d cluster
	-k3d cluster delete $(CLUSTER)
	-rm -f kube/config

destroy: ## Stop and delete everything: compose volumes AND k3d cluster
	-docker compose down -v --remove-orphans
	-k3d cluster delete $(CLUSTER)
	-rm -f kube/config

build: ## Build Go binaries locally (no Docker)
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/control-plane ./cmd/control-plane
	CGO_ENABLED=0 go build -o bin/build-service ./cmd/build-service

test: ## Run unit tests
	go test ./...

fmt: ## go fmt
	go fmt ./...

vet: ## go vet
	go vet ./...
