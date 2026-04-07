.PHONY: help build docs code manifests test

# Default target
help:
	@echo "MikroLB Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  build      - Build the container image"
	@echo "  docs       - Generate documentation"
	@echo "  code       - Generate API code"
	@echo "  manifests  - Generate Kubernetes manifests"
	@echo "  test       - Run all tests"

export PATH := $(PATH):$(shell go env GOPATH)/bin

# Go parameters
GOCMD=go
CONTAINER_ENGINE=$(notdir $(shell which podman 2>/dev/null || which docker 2>/dev/null))
IMAGE_NAME=ghcr.io/gerolf-vent/mikrolb-controller
IMAGE_VERSION=0.1.0

build: code
	@echo "Building container image..."
	@$(CONTAINER_ENGINE) image build -f ./Containerfile --tag "$(IMAGE_NAME):$(IMAGE_VERSION)" .

docs:
	@echo "Installing crd-ref-docs..."
	@$(GOCMD) install github.com/elastic/crd-ref-docs@latest
	@echo "Generating K8s API docs..."
	@crd-ref-docs --source-path=./api/v1alpha1 --config=docs/.crd-ref-docs/config.yaml --renderer=markdown --templates-dir=docs/.crd-ref-docs/templates --output-path=docs/reference/api.md

code:
	@echo "Installing controller-gen..."
	@$(GOCMD) install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
	@echo "Generating DeepCopy methods..."
	@controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests:
	@echo "Installing controller-gen..."
	@$(GOCMD) install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
	@echo "Generating CRD manifests..."
	@controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

setup-envtest:
	@echo "Installing setup-envtest tool..."
	@$(GOCMD) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@echo "Downloading and setting up envtest binaries..."
	@setup-envtest use -p path

test: setup-envtest
	@echo "Running all tests..."
	$(GOCMD) test -v ./...
