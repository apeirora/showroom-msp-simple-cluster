SHELL:=/usr/bin/env bash
.DEFAULT_GOAL:=help

ifdef GITHUB_REPOSITORY
GHCR_REPO := $(shell echo "$(GITHUB_REPOSITORY)" | tr '[:upper:]' '[:lower:]')
IMAGE := ghcr.io/$(GHCR_REPO)/bind-watcher-operator
else
IMAGE := ghcr.io/apeirora/bind-watcher-operator
endif

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: tidy
tidy: ## Ensure go modules are up to date.
	go mod tidy

.PHONY: build
build: ## Build the binary.
	go build -o bin/manager ./cmd

.PHONY: test
test: ## Run tests.
	go test -race -covermode=atomic -coverprofile=cover.out ./...

.PHONY: lint
lint: ## Vet the codebase.
	go vet ./...

.PHONY: build-image
build-image: ## Build the container image.
	docker build -t $(IMAGE) .

.PHONY: push-image
push-image: build-image ## Build and push the image (VERSION required).
	docker tag $(IMAGE) $(IMAGE):$(VERSION)
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE)
