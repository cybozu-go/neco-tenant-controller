# Tool versions
CTRL_TOOLS_VERSION=0.7.0
CTRL_RUNTIME_VERSION := $(shell awk '/sigs.k8s.io\/controller-runtime/ {print substr($$2, 2)}' go.mod)
KUSTOMIZE_VERSION = 4.4.1

# Test tools
BIN_DIR := $(shell pwd)/bin
STATICCHECK := $(BIN_DIR)/staticcheck
NILERR := $(BIN_DIR)/nilerr
SUDO = sudo

# Set the shell used to bash for better error handling.
SHELL = /bin/bash
.SHELLFLAGS = -e -o pipefail -c

CRD_OPTIONS = "crd:crdVersions=v1,maxDescLen=220"

# for Go
GOOS = $(shell go env GOOS)
GOARCH = $(shell go env GOARCH)
SUFFIX =

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: kustomize controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: check-generate
check-generate:
	$(MAKE) manifests generate apidoc
	git diff --exit-code --name-only

.PHONY: envtest
envtest: setup-envtest
	source <($(SETUP_ENVTEST) use -p env); \
		go test -v -count 1 -race ./controllers -ginkgo.progress -ginkgo.v -ginkgo.failFast

.PHONY: test
test: test-tools
	go test -v -count 1 -race ./pkg/...
	go install ./...
	go vet ./...
	test -z $$(gofmt -s -l . | tee /dev/stderr)
	$(STATICCHECK) ./...
	$(NILERR) ./...

##@ Build

.PHONY: build
build:
	mkdir -p bin
	GOBIN=$(shell pwd)/bin go install ./cmd/...

.PHONY: docker-build
docker-build:
	docker build -t neco-tenant-controller:latest .

##@ Tools

CONTROLLER_GEN := $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v$(CTRL_TOOLS_VERSION))

SETUP_ENVTEST := $(shell pwd)/bin/setup-envtest
.PHONY: setup-envtest
setup-envtest: $(SETUP_ENVTEST) ## Download setup-envtest locally if necessary
$(SETUP_ENVTEST):
	# see https://github.com/kubernetes-sigs/controller-runtime/tree/master/tools/setup-envtest
	GOBIN=$(shell pwd)/bin go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

KUSTOMIZE := $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.

$(KUSTOMIZE):
	mkdir -p bin
	curl -fsL https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv$(KUSTOMIZE_VERSION)/kustomize_v$(KUSTOMIZE_VERSION)_linux_amd64.tar.gz | \
	tar -C bin -xzf -

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
}
endef

.PHONY: test-tools
test-tools: $(STATICCHECK) $(NILERR)

$(STATICCHECK):
	mkdir -p $(BIN_DIR)
	GOBIN=$(BIN_DIR) go install honnef.co/go/tools/cmd/staticcheck@latest

$(NILERR):
	mkdir -p $(BIN_DIR)
	GOBIN=$(BIN_DIR) go install github.com/gostaticanalysis/nilerr/cmd/nilerr@latest
