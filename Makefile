# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.16.0

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

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
export PATH  := $(PATH):$(PWD)/bin
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

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen generate-code ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

generate-code: ## Generate code containing Clientset, Informers, Listers
	@echo "Running generate-code"
	hack/update-codegen.sh

.PHONY: fmt
fmt: ## Run go fmt against code.
	@echo "Running go fmt"
	go fmt ./...

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint against code.
	@echo "Running golangci-lint"
	hack/golangci-lint.sh

.PHONY: vet
vet: ## Run go vet against code.
	@echo "Running go vet"
	go vet ./...

.PHONY: lint
lint: ## Run golint against code.
	@echo "Running golint"
	hack/lint.sh

.PHONY: unittests
unittests: pre-cache-unit-test
	@echo "Running unittests"
	go test -v ./controllers/...
	@echo "Running backup unittests"
	go test -v ./recovery/cmd/...
	
.PHONY: common-deps-update
common-deps-update:	controller-gen kustomize
	go mod tidy

.PHONY: shellcheck
shellcheck: ## Run shellcheck
	@echo "Running shellcheck"
	hack/shellcheck.sh

.PHONY: bashate
bashate: ## Run bashate
	@echo "Running bashate"
	hack/bashate.sh

.PHONY: ci-job
ci-job: common-deps-update generate fmt vet lint golangci-lint unittests verify-bindata shellcheck bashate bundle-check

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
KUSTOMIZE = $(shell pwd)/bin/kustomize

.PHONY: kind-deps-update
kind-deps-update: common-deps-update
	hack/install-integration-tests-deps.sh kind

.PHONY: non-kind-deps-update
non-kind-deps-update: common-deps-update
	hack/install-integration-tests-deps.sh non-kind

controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.13.0)

OPERATOR_SDK_VERSION = $(shell $(OPERATOR_SDK) version 2>/dev/null | sed 's/^operator-sdk version: "\([^"]*\).*/\1/')
OPERATOR_SDK_VERSION_REQ = v1.16.0-ocp
operator-sdk: ## Download operator-sdk locally if necessary.
ifneq ($(OPERATOR_SDK_VERSION_REQ),$(OPERATOR_SDK_VERSION))
	curl -L https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/operator-sdk/4.10.17/operator-sdk-v1.16.0-ocp-linux-x86_64.tar.gz | tar -xz -C bin/
endif

kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.4)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(firstword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
go mod tidy ;\
rm -rf $$TMP_DIR ;\
}
endef

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
	${ENGINE} build -t ${IMG} -f Dockerfile .

docker-push: ## Push container image with the manager.
	${ENGINE} push ${IMG}

docker-build-precache: ## Build pre-cache workload container image.
	${ENGINE} build -t ${PRECACHE_IMG} -f Dockerfile.precache .

docker-push-precache: ## push pre-cache workload container image.
	${ENGINE} push ${PRECACHE_IMG}

docker-build-recovery: ## Build recovery container image
	${ENGINE} build -t ${RECOVERY_IMG} -f Dockerfile.recovery .

docker-push-recovery: ## Push recovery container image.
	${ENGINE} push ${RECOVERY_IMG}

docker-build-aztp: ## Build aztp container image
	${ENGINE} build -t ${AZTP_IMG} -f Dockerfile.aztp .

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
	hack/check-git-tree.sh

.PHONY: bundle-run
bundle-run: # Install bundle on cluster using operator sdk. Index image is require due to upstream issue: https://github.com/operator-framework/operator-registry/issues/984
	$(OPERATOR_SDK) --index-image=quay.io/operator-framework/opm:v1.23.0 run bundle $(BUNDLE_IMG)

.PHONY: bundle-upgrade
bundle-upgrade: # Upgrade bundle on cluster using operator sdk.
	$(OPERATOR_SDK) run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-clean
bundle-clean: # Uninstall bundle on cluster using operator sdk.
	$(OPERATOR_SDK) cleanup cluster-group-upgrades-operator

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.15.1/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

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

uninstall-acm-crds:
	kubectl delete -f deploy/acm/crds/apps.open-cluster-management.io_placementrules_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_placementbindings_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policies_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policyautomations_crd.yaml
	kubectl delete -f deploy/acm/crds/cluster.open-cluster-management.io_managedclusters.yaml
	kubectl delete -f deploy/acm/crds/view.open-cluster-management.io_managedclusterviews.yaml
	kubectl delete -f deploy/acm/crds/action.open-cluster-management.io_managedclusteractions.yaml

kuttl-test: ## Run KUTTL tests
	@echo "Running KUTTL tests"
	kubectl-kuttl test

start-test-proxy:
	@echo "Start kubectl proxy for testing"
	./hack/start_kubectl_proxy.sh

stop-test-proxy:
	@echo "Stop kubectl proxy for testing"
	cat ./bin/kubectl_proxy_pid | xargs kill -9

kind-complete-deployment: kind-deps-update kind-bootstrap-cluster docker-build kind-load-operator-image deploy ## Deploy cluster with the Upgrades operator
kind-complete-kuttl-test: kind-complete-deployment kuttl-test stop-test-proxy kind-delete-cluster ## Deploy cluster with the Upgrades operator and run KUTTL tests

complete-deployment: non-kind-deps-update start-test-proxy install-acm-crds install
complete-kuttl-test: complete-deployment kuttl-test stop-test-proxy

pre-cache-unit-test: ## Run pre-cache scripts unit tests
	cwd=pre-cache ./pre-cache/test.sh
