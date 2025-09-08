# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.18.0

# BASHATE_VERSION defines the bashate version to download from GitHub releases.
BASHATE_VERSION ?= 2.1.1

# CONTROLLER_GEN_VERSION defines the controller-gen version to download from go modules.
CONTROLLER_GEN_VERSION ?= v0.14.0

# GOLANGCI_LINT_VERSION defines the golangci-lint version to download from GitHub releases.
GOLANGCI_LINT_VERSION ?= v1.52.0

# KUSTOMIZE_VERSION defines the kustomize version to download from go modules.
KUSTOMIZE_VERSION ?= v4@v4.5.4

# OPERATOR_SDK_VERSION defines the operator-sdk version to download from GitHub releases.
OPERATOR_SDK_VERSION ?= 1.28.0

# OPM_VERSION defines the opm version to download from GitHub releases.
OPM_VERSION ?= v1.52.0

# SHELLCHECK_VERSION defines the shellcheck version to download from GitHub releases.
SHELLCHECK_VERSION ?= v0.11.0

# YAMLLINT_VERSION defines the yamllint version to download from GitHub releases.
YAMLLINT_VERSION ?= 1.37.1

# YQ_VERSION defines the yq version to download from GitHub releases.
YQ_VERSION ?= v4.45.4

# You can use podman or docker as a container engine. Notice that there are some options that might be only valid for one of them.
ENGINE ?= docker

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "preview,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/cluster-group-upgrades-operator-bundle:$VERSION and openshift.io/cluster-group-upgrades-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/openshift-kni/cluster-group-upgrades-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)
PRECACHE_IMG ?= $(IMAGE_TAG_BASE)-precache:$(VERSION)
RECOVERY_IMG ?= $(IMAGE_TAG_BASE)-recovery:$(VERSION)
AZTP_IMG ?= $(IMAGE_TAG_BASE)-aztp:$(VERSION)

CRD_OPTIONS ?= "crd"

# Konflux catalog configuration
PACKAGE_NAME_KONFLUX = topology-aware-lifecycle-manager
CATALOG_TEMPLATE_KONFLUX_INPUT = .konflux/catalog/catalog-template.in.yaml
CATALOG_TEMPLATE_KONFLUX_OUTPUT = .konflux/catalog/catalog-template.out.yaml
CATALOG_KONFLUX = .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/catalog.yaml

# Konflux bundle image configuration
BUNDLE_NAME_SUFFIX = bundle-4-18
PRODUCTION_BUNDLE_NAME = operator-bundle

# By default we build the same architecture we are running
# Override this by specifying a different GOARCH in your environment
HOST_ARCH ?= $(shell uname -m)

# Convert from uname format to GOARCH format
ifeq ($(HOST_ARCH),aarch64)
	HOST_ARCH=arm64
endif
ifeq ($(HOST_ARCH),x86_64)
	HOST_ARCH=amd64
endif

# Define GOARCH as HOST_ARCH if not otherwise defined
ifndef GOARCH
	GOARCH=$(HOST_ARCH)
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Get the directory of the current makefile
# Trim any trailing slash from the directory path as we will add if when necessary later
PROJECT_DIR := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

## Location to install dependencies to
# If you are setting this externally then you must use an absolute path
LOCALBIN ?= $(PROJECT_DIR)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
# Prefer binaries in the local bin directory over system binaries.
export PATH := $(abspath $(LOCALBIN)):$(PATH)
GOFLAGS := -mod=mod
SHELL = /usr/bin/env GOFLAGS=$(GOFLAGS) bash -o pipefail

.SHELLFLAGS = -ec

# Include the bindata makefile
include ./vendor/github.com/openshift/build-machinery-go/make/targets/openshift/bindata.mk

# This will call a macro called "add-bindata" which will generate bindata specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - input dirs
# $3 - prefix
# $4 - pkg
# $5 - output
# It will generate targets {update,verify}-bindata-$(1) logically grouping them in unsuffixed versions of these targets
# and also hooked into {update,verify}-generated for broader integration.
$(call add-bindata,recovery,./recovery/bindata/...,recovery/bindata,generated,recovery/generated/zz_generated.bindata.go)

# Kind configuration
KIND_NAME ?= test-upgrades-operator
KIND_ACM_NAMESPACE ?= open-cluster-management
KIND_VERSION ?= latest
ifneq ($(KIND_VERSION), latest)
	KIND_ARGS = --image kindest/node:$(KIND_VERSION)
else
	KIND_ARGS =
endif
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

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen generate-code ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

generate-code: ## Generate code containing Clientset, Informers, Listers
	@echo "Running generate-code"
	$(PROJECT_DIR)/hack/update-codegen.sh

.PHONY: fmt
fmt: ## Run go fmt against code.
	@echo "Running go fmt"
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	@echo "Running go vet"
	go vet ./...

.PHONY: unittests
unittests: pre-cache-unit-test
	@echo "Running unittests"
	go test -v ./controllers/...
	@echo "Running backup unittests"
	go test -v ./recovery/cmd/...
	
.PHONY: common-deps-update
common-deps-update:	controller-gen kustomize
	go mod tidy


.PHONY: ci-job
ci-job: common-deps-update generate fmt vet golangci-lint unittests verify-bindata shellcheck bashate yamllint bundle-check

# Set the paths to the binaries in the local bin directory
BASHATE = $(LOCALBIN)/bashate
CONTROLLER_GEN = $(LOCALBIN)/controller-gen
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
KUSTOMIZE = $(LOCALBIN)/kustomize
OPERATOR_SDK = $(LOCALBIN)/operator-sdk
OPM = $(LOCALBIN)/opm
SHELLCHECK = $(LOCALBIN)/shellcheck
YAMLLINT = $(LOCALBIN)/yamllint
YQ = $(LOCALBIN)/yq

.PHONY: kind-deps-update
kind-deps-update: common-deps-update
	$(PROJECT_DIR)/hack/install-integration-tests-deps.sh kind

.PHONY: non-kind-deps-update
non-kind-deps-update: common-deps-update
	$(PROJECT_DIR)/hack/install-integration-tests-deps.sh non-kind

# Download go tools
.PHONY: controller-gen
controller-gen: sync-git-submodules $(LOCALBIN) ## Download controller-gen locally if necessary.
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-go-tool \
		TOOL_NAME=controller-gen \
		GO_MODULE=sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN)

.PHONY: kustomize
kustomize: sync-git-submodules $(LOCALBIN) ## Download kustomize locally if necessary.
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-go-tool \
		TOOL_NAME=kustomize \
		GO_MODULE=sigs.k8s.io/kustomize/kustomize/$(KUSTOMIZE_VERSION) \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN)

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: manifests generate fmt vet ## Run tests. (needs make kind-complete-deployment)
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.8.3/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./... -v -coverprofile cover.out

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	PRECACHE_IMG=${PRECACHE_IMG} RECOVERY_IMG=${RECOVERY_IMG} AZTP_IMG=$(AZTP_IMG) go run ./main.go

debug: manifests generate fmt vet ## Run a controller from your host that accepts remote attachment.
	PRECACHE_IMG=${PRECACHE_IMG} RECOVERY_IMG=${RECOVERY_IMG} AZTP_IMG=$(AZTP_IMG) dlv debug --headless --listen 127.0.0.1:2345 --api-version 2 --accept-multiclient ./main.go

docker-build: ## Build container image with the manager.
	${ENGINE} build -t ${IMG} --arch=${GOARCH} --build-arg GOARCH=${GOARCH} -f Dockerfile .

docker-push: ## Push container image with the manager.
	${ENGINE} push ${IMG}

docker-build-precache: ## Build pre-cache workload container image.
	${ENGINE} build -t ${PRECACHE_IMG} -f Dockerfile.precache .

docker-push-precache: ## push pre-cache workload container image.
	${ENGINE} push ${PRECACHE_IMG}

docker-build-recovery: ## Build recovery container image
	${ENGINE} build -t ${RECOVERY_IMG} --arch=${GOARCH} --build-arg GOARCH=${GOARCH} -f Dockerfile.recovery .

docker-push-recovery: ## Push recovery container image.
	${ENGINE} push ${RECOVERY_IMG}

docker-build-aztp: ## Build aztp container image
	${ENGINE} build -t ${AZTP_IMG} --arch=${GOARCH} --build-arg GOARCH=${GOARCH} -f Dockerfile.aztp .

docker-push-aztp: ## Push recovery container image.
	${ENGINE} push ${AZTP_IMG}
##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG) && AZTP_IMG=$(AZTP_IMG) PRECACHE_IMG=$(PRECACHE_IMG) RECOVERY_IMG=$(RECOVERY_IMG) envsubst < related-images/in.yaml > related-images/patch.yaml 
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: bundle
bundle: operator-sdk manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests --apis-dir pkg/api/ -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG) && AZTP_IMG=$(AZTP_IMG) PRECACHE_IMG=$(PRECACHE_IMG) RECOVERY_IMG=$(RECOVERY_IMG) envsubst < related-images/in.yaml > related-images/patch.yaml 
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle
	sed -i '/^[[:space:]]*createdAt:/d' bundle/manifests/cluster-group-upgrades-operator.clusterserviceversion.yaml

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	${ENGINE} build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: bundle-check
bundle-check: bundle
# Workaround for CI which adds phantom dependencies to go.sum
	go mod tidy
	$(PROJECT_DIR)/hack/check-git-tree.sh

.PHONY: bundle-run
bundle-run: # Install bundle on cluster using operator sdk. Index image is require due to upstream issue: https://github.com/operator-framework/operator-registry/issues/984
	$(OPERATOR_SDK) run bundle $(BUNDLE_IMG)

.PHONY: bundle-upgrade
bundle-upgrade: # Upgrade bundle on cluster using operator sdk.
	$(OPERATOR_SDK) run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-clean
bundle-clean: # Uninstall bundle on cluster using operator sdk.
	$(OPERATOR_SDK) cleanup cluster-group-upgrades-operator

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool $(ENGINE) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

############################################################
# Cluster deployment test section
############################################################
##@ Test

.PHONY: kind-bootstrap-cluster
kind-bootstrap-cluster: kind-create-cluster start-test-proxy install-acm-crds ## Deploy kind cluster and dependencies

# Specify KIND_VERSION to indicate the version tag of the KinD image
kind-create-cluster:
	@echo "Creating kind cluster"
	kind create cluster --name $(KIND_NAME) $(KIND_ARGS)

kind-delete-cluster: ## Delete kind cluster
	kind delete cluster --name $(KIND_NAME)

kind-load-operator-image: ## Load Upgrades operator image to kind cluster
	@echo "Load Upgrades operator image to kind cluster"
	kind load docker-image ${IMG} --name ${KIND_NAME}

# ACM specific CRDs have to exist before being able to start running the tests.
install-acm-crds:
	@echo "Installing ACM CRDs"
	kubectl apply -f deploy/acm/crds/apps.open-cluster-management.io_placementrules_crd.yaml
	kubectl apply -f deploy/acm/crds/policy.open-cluster-management.io_placementbindings_crd.yaml
	kubectl apply -f deploy/acm/crds/policy.open-cluster-management.io_policies_crd.yaml
	kubectl apply -f deploy/acm/crds/policy.open-cluster-management.io_policyautomations_crd.yaml
	kubectl apply -f deploy/acm/crds/cluster.open-cluster-management.io_managedclusters.yaml
	kubectl apply -f deploy/acm/crds/view.open-cluster-management.io_managedclusterviews.yaml
	kubectl apply -f deploy/acm/crds/action.open-cluster-management.io_managedclusteractions.yaml
	kubectl apply -f deploy/acm/crds/work.open-cluster-management.io_manifestworks.crd.yaml
	kubectl apply -f deploy/acm/crds/work.open-cluster-management.io_manifestworkreplicasets.crd.yaml

uninstall-acm-crds:
	kubectl delete -f deploy/acm/crds/apps.open-cluster-management.io_placementrules_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_placementbindings_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policies_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policyautomations_crd.yaml
	kubectl delete -f deploy/acm/crds/cluster.open-cluster-management.io_managedclusters.yaml
	kubectl delete -f deploy/acm/crds/view.open-cluster-management.io_managedclusterviews.yaml
	kubectl delete -f deploy/acm/crds/action.open-cluster-management.io_managedclusteractions.yaml
	kubectl delete -f deploy/acm/crds/work.open-cluster-management.io_manifestworks.crd.yaml
	kubectl delete -f deploy/acm/crds/work.open-cluster-management.io_manifestworkreplicasets.crd.yaml

kuttl-test: ## Run KUTTL tests
	@echo "Running KUTTL tests"
	kubectl-kuttl test --namespace default

start-test-proxy:
	@echo "Start kubectl proxy for testing"
	$(PROJECT_DIR)/hack/start_kubectl_proxy.sh

stop-test-proxy:
	@echo "Stop kubectl proxy for testing"
	cat ./bin/kubectl_proxy_pid | xargs kill -9

kind-complete-deployment: kind-deps-update kind-bootstrap-cluster docker-build kind-load-operator-image deploy ## Deploy cluster with the Upgrades operator
kind-complete-kuttl-test: kind-complete-deployment kuttl-test stop-test-proxy kind-delete-cluster ## Deploy cluster with the Upgrades operator and run KUTTL tests

complete-deployment: non-kind-deps-update start-test-proxy install-acm-crds install
complete-kuttl-test: complete-deployment kuttl-test stop-test-proxy

pre-cache-unit-test: ## Run pre-cache scripts unit tests
	cwd=pre-cache ./pre-cache/test.sh

##@ Tools and linting

.PHONY: lint
lint: bashate golangci-lint shellcheck yamllint markdownlint

.PHONY: tools
tools: opm operator-sdk yq

.PHONY: bashate-download
bashate-download: sync-git-submodules $(LOCALBIN) ## Download bashate locally if necessary and run against bash files. If wrong version is installed, it will be removed before downloading.
	@echo "Downloading bashate..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download \
		download-bashate \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_BASHATE_VERSION=$(BASHATE_VERSION)
	@echo "Bashate downloaded successfully."

.PHONY: bashate
bashate: bashate-download $(BASHATE) ## Lint bash files in the repository
	@echo "Running bashate on repository bash files..."
	find $(PROJECT_DIR) -name '*.sh' \
		-not -path '$(PROJECT_DIR)/vendor/*' \
		-not -path '$(PROJECT_DIR)/*/vendor/*' \
		-not -path '$(PROJECT_DIR)/git/*' \
		-not -path '$(LOCALBIN)/*' \
		-not -path '$(PROJECT_DIR)/testbin/*' \
		-not -path '$(PROJECT_DIR)/telco5g-konflux/*' \
		-print0 \
		| xargs -0 --no-run-if-empty $(BASHATE) -v -e 'E*' -i E006
	@echo "Bashate linting completed successfully."

.PHONY: golangci-lint-download
golangci-lint-download: sync-git-submodules $(LOCALBIN) ## Download golangci-lint locally if necessary. If wrong version is installed, it will be removed before downloading.
	@echo "Downloading golangci-lint..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download \
		download-go-tool \
		TOOL_NAME=golangci-lint \
		GO_MODULE=github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN)
	@echo "Golangci-lint downloaded successfully."

.PHONY: golangci-lint
golangci-lint: golangci-lint-download $(GOLANGCI_LINT) ## Run golangci-lint against code.
	@echo "Running golangci-lint on repository go files..."
	$(GOLANGCI_LINT) run -v
	@echo "Golangci-lint linting completed successfully."

operator-sdk: sync-git-submodules $(LOCALBIN) ## Download operator-sdk locally if necessary.
	@$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-operator-sdk \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_OPERATOR_SDK_VERSION=$(OPERATOR_SDK_VERSION)
	@echo "Operator sdk downloaded successfully."

.PHONY: opm
opm: sync-git-submodules $(LOCALBIN) ## Download opm locally if necessary.
	@$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-opm \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_OPM_VERSION=$(OPM_VERSION)
	@echo "Opm downloaded successfully."

.PHONY: shellcheck-download
shellcheck-download: sync-git-submodules $(LOCALBIN) ## Download shellcheck locally if necessary. If wrong version is installed, it will be removed before downloading.
	@echo "Downloading shellcheck..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download \
		download-shellcheck \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_SHELLCHECK_VERSION=$(SHELLCHECK_VERSION)
	@echo "Shellcheck downloaded successfully."

.PHONY: shellcheck
shellcheck: shellcheck-download $(SHELLCHECK) ## Lint bash files in the repository
	@echo "Running shellcheck on repository bash files..."
	find $(PROJECT_DIR) -name '*.sh' \
		-not -path '$(PROJECT_DIR)/vendor/*' \
		-not -path '$(PROJECT_DIR)/*/vendor/*' \
		-not -path '$(PROJECT_DIR)/git/*' \
		-not -path '$(LOCALBIN)/*' \
		-not -path '$(PROJECT_DIR)/testbin/*' \
		-not -path '$(PROJECT_DIR)/telco5g-konflux/*' \
		-print0 \
		| xargs -0 --no-run-if-empty $(SHELLCHECK) -x
	@echo "Shellcheck linting completed successfully."

.PHONY: yamllint-download
yamllint-download: sync-git-submodules $(LOCALBIN) ## Download yamllint locally if necessary and run against yaml files. If wrong version is installed, it will be removed before downloading.
	@echo "Downloading yamllint..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download \
		download-yamllint \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_YAMLLINT_VERSION=$(YAMLLINT_VERSION)
	@echo "Yamllint downloaded successfully."

.PHONY: yamllint
yamllint: yamllint-download $(YAMLLINT) ## Lint YAML files in the repository
	@echo "Running yamllint on repository YAML files..."
	$(YAMLLINT) -c $(PROJECT_DIR)/.yamllint.yaml $(PROJECT_DIR)
	@echo "YAML linting completed successfully."

.PHONY: yq
yq: sync-git-submodules $(LOCALBIN) ## Download yq
	@echo "Downloading yq..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/download download-yq \
		DOWNLOAD_INSTALL_DIR=$(LOCALBIN) \
		DOWNLOAD_YQ_VERSION=$(YQ_VERSION)
	@echo "Yq downloaded successfully."

.PHONY: yq-sort-and-format
yq-sort-and-format: yq ## Sort keys/reformat all YAML files in the repository
	@echo "Sorting keys and reformatting YAML files..."
	@find . -name "*.yaml" -o -name "*.yml" | grep -v -E "(telco5g-konflux/|target/|vendor/|bin/|\.git/)" | while read file; do \
		echo "Processing $$file..."; \
		$(YQ) -i '.. |= sort_keys(.)' "$$file"; \
	done
	@echo "YAML sorting and formatting completed successfully."

##@ Konflux

.PHONY: sync-git-submodules
sync-git-submodules:
	@echo "Checking git submodules"
	@if [ "$(SKIP_SUBMODULE_SYNC)" != "yes" ]; then \
		echo "Syncing git submodules"; \
		git submodule sync --recursive; \
		git submodule update --init --recursive; \
	else \
		echo "Skipping submodule sync"; \
	fi

.PHONY: konflux-fix-catalog-name
konflux-fix-catalog-name: ## Fix catalog package name for TALM
	if [ "$$(uname)" = "Darwin" ]; then \
		sed -i '' 's/cluster-group-upgrades-operator/topology-aware-lifecycle-manager/g' .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/catalog.yaml; \
	else \
		sed -i 's/cluster-group-upgrades-operator/topology-aware-lifecycle-manager/g' .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/catalog.yaml; \
	fi

.PHONY: konflux-validate-catalog-template-bundle
konflux-validate-catalog-template-bundle: sync-git-submodules yq operator-sdk ## validate the last bundle entry on the catalog template file
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-validate-catalog-template-bundle \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		YQ=$(YQ) \
		OPERATOR_SDK=$(OPERATOR_SDK) \
		ENGINE=$(ENGINE)

.PHONY: konflux-validate-catalog
konflux-validate-catalog: sync-git-submodules opm ## validate the current catalog file
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-validate-catalog \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		OPM=$(OPM)

.PHONY: konflux-generate-catalog
konflux-generate-catalog: sync-git-submodules yq opm ## generate a quay.io catalog
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-generate-catalog \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		BUNDLE_BUILDS_FILE=$(PROJECT_DIR)/.konflux/catalog/bundle.builds.in.yaml \
		OPM=$(OPM) \
		YQ=$(YQ)
	$(MAKE) konflux-fix-catalog-name
	$(MAKE) konflux-validate-catalog

.PHONY: konflux-generate-catalog-production
konflux-generate-catalog-production: sync-git-submodules yq opm ## generate a registry.redhat.io catalog
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-generate-catalog-production \
		CATALOG_TEMPLATE_KONFLUX_INPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_INPUT) \
		CATALOG_TEMPLATE_KONFLUX_OUTPUT=$(PROJECT_DIR)/$(CATALOG_TEMPLATE_KONFLUX_OUTPUT) \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		BUNDLE_NAME_SUFFIX=$(BUNDLE_NAME_SUFFIX) \
		PRODUCTION_BUNDLE_NAME=$(PRODUCTION_BUNDLE_NAME) \
		BUNDLE_BUILDS_FILE=$(PROJECT_DIR)/.konflux/catalog/bundle.builds.in.yaml \
		OPM=$(OPM) \
		YQ=$(YQ)
	$(MAKE) konflux-fix-catalog-name
	$(MAKE) konflux-validate-catalog

.PHONY: konflux-update-tekton-task-refs
konflux-update-tekton-task-refs: sync-git-submodules ## Update task references in Tekton pipeline files
	@echo "Updating task references in Tekton pipeline files..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/tekton update-task-refs \
		PIPELINE_FILES="$$(find $(PROJECT_DIR)/.tekton -type f \( -name '*.yaml' -o -name '*.yml' \) -print0 | xargs -0 -r printf '%s ')"
	@echo "Task references updated successfully."

.PHONY: konflux-compare-catalog
konflux-compare-catalog: sync-git-submodules ## Compare generated catalog with upstream FBC image
	@echo "Comparing generated catalog with upstream FBC image..."
	$(MAKE) -C $(PROJECT_DIR)/telco5g-konflux/scripts/catalog konflux-compare-catalog \
		CATALOG_KONFLUX=$(PROJECT_DIR)/$(CATALOG_KONFLUX) \
		PACKAGE_NAME_KONFLUX=$(PACKAGE_NAME_KONFLUX) \
		UPSTREAM_FBC_IMAGE=quay.io/redhat-user-workloads/telco-5g-tenant/$(PACKAGE_NAME_KONFLUX)-fbc-4-18:latest

.PHONY: konflux-all
konflux-all: konflux-update-tekton-task-refs konflux-generate-catalog-production konflux-validate-catalog ## Run all Konflux-related targets
	@echo "All Konflux targets completed successfully."

help:   ## Shows this message.
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

clean:
	rm -rf $(LOCALBIN)
