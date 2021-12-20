# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.3

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

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/cluster-group-upgrades-operator-bundle:$VERSION and openshift.io/cluster-group-upgrades-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/openshift-kni/cluster-group-upgrades-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)
PRECACHE_IMG ?= $(IMAGE_TAG_BASE)-precache:$(VERSION)

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
PATH  := $(PATH):$(PWD)/bin
GOFLAGS := -mod=mod
SHELL = /usr/bin/env PATH=$(PATH) GOFLAGS=$(GOFLAGS) bash -o pipefail

.SHELLFLAGS = -ec

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

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	@echo "Running go fmt"
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	@echo "Running go vet"
	go vet ./...

.PHONY: lint
lint: ## Run golint against code.
	@echo "Running golint"
	hack/lint.sh

.PHONY: common-deps-update
common-deps-update:	controller-gen kustomize
	go mod tidy

.PHONY: ci-job
ci-job: common-deps-update fmt vet lint

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
KUSTOMIZE = $(shell pwd)/bin/kustomize

.PHONY: kind-deps-update
kind-deps-update: common-deps-update
	hack/install-integration-tests-deps.sh kind

.PHONY: non-kind-deps-update
non-kind-deps-update: common-deps-update
	hack/install-integration-tests-deps.sh non-kind

controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
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
	go run ./main.go

docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} -f Dockerfile .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

docker-build-precache: ## Build pre-cache workload docker image.
	docker build -t ${PRECACHE_IMG} -f Dockerfile.precache .

docker-push-precache: ## push pre-cache workload docker image.
	docker push ${PRECACHE_IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: bundle
bundle: manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

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
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

############################################################
# Cluster deployment test section
############################################################
##@ Test

.PHONY: kind-bootstrap-cluster
kind-bootstrap-cluster: kind-create-cluster install-acm-crds deploy-policy-propagator-controller ## Deploy kind cluster and dependencies

# In order to use the Upgrades operator we need the policy controller to run so that
# all the needed upgrades policies and placements are created.
deploy-policy-propagator-controller:
	@echo "Installing policy-propagator"
	kubectl create ns ${KIND_ACM_NAMESPACE}
	kubectl apply -f deploy/acm/ -n $(KIND_ACM_NAMESPACE)

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

uninstall-acm-crds:
	kubectl delete -f deploy/acm/crds/apps.open-cluster-management.io_placementrules_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_placementbindings_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policies_crd.yaml
	kubectl delete -f deploy/acm/crds/policy.open-cluster-management.io_policyautomations_crd.yaml
	kubectl delete -f deploy/acm/crds/cluster.open-cluster-management.io_managedclusters.yaml

kuttl-test: ## Run KUTTL tests
	@echo "Running KUTTL tests"
	kubectl-kuttl test

kind-complete-deployment: kind-deps-update kind-bootstrap-cluster docker-build kind-load-operator-image deploy ## Deploy cluster with the Upgrades operator
kind-complete-kuttl-test: kind-complete-deployment kuttl-test kind-delete-cluster ## Deploy cluster with the Upgrades operator and run KUTTL tests

complete-deployment: non-kind-deps-update install-acm-crds deploy-policy-propagator-controller install
complete-kuttl-test: complete-deployment kuttl-test

pre-cache-unit-test: ## Run pre-cache scripts unit tests
	cwd=pre-cache ./pre-cache/test.sh
