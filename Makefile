.PHONY: help code manifests test

# Default target
help:
	@echo "MikroLB Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  code       - Generate API code"
	@echo "  manifests  - Generate Kubernetes manifests"
	@echo "  test       - Run all tests"

export PATH := $(PATH):$(shell go env GOPATH)/bin

# Go parameters
GOCMD=go

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
