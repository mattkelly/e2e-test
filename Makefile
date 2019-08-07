SHELL=/bin/bash
PROJECT_NAME ?= "e2e-test"
IMAGE_NAME ?= "containership/$(PROJECT_NAME)"
IMAGE_TAG ?= "latest"
PKG_LIST := $(shell go list ./...)
GO_FILES := $(shell find . -type f -not -path './vendor/*' -name '*.go')

.PHONY: all
all: build ## (default) Build

.PHONY: check
check: fmt-check golangci ## Run all checkers

.PHONY: golangci
golangci: ## Run GolangCI checks
	@golangci-lint run

.PHONY: fmt-check
fmt-check: ## Check the file format
	@gofmt -s -e -d $(GO_FILES) | read; \
		if [ $$? == 0 ]; then \
			echo "gofmt check failed:"; \
			gofmt -s -e -d $(GO_FILES); \
			exit 1; \
		fi

.PHONY: test
test: ## Run unit tests
	@go test -short ${PKG_LIST}

.PHONY: coverage
coverage: ## Run unit tests with coverage checking / codecov integration
	@go test -short -coverprofile=coverage.txt -covermode=count ${PKG_LIST}

.PHONY: vet
vet: ## Vet the files
	@go vet ${PKG_LIST}

.PHONY: help
help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the Docker image
	@docker image build -t $(IMAGE_NAME):$(IMAGE_TAG) .
