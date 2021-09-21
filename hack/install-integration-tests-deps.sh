#!/bin/bash

set -e

_GOOS=$(go env GOOS)
_GOARCH=$(go env GOARCH)
_ARCH=$(arch)

KIND_ENV="kind"
NON_KIND_ENV="non-kind"
mkdir -p ./bin


if [ -z "$1" ]; then
    echo "kind / non-kind parameter expected, exit"
    exit 1
fi

env=$1

# Install kubectl if needed.
if ! [ -x "$(command -v kubectl)" ]; then
    echo "Installing kubectl..."
    curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/$_GOOS/$_GOARCH/kubectl
    mv ./kubectl ./bin/
    chmod 755 ./bin/kubectl
else
    echo "No need to install kubectl, continue..."
fi

# Install kind if needed.
if [ "$env" == "$KIND_ENV" ] && ! [ -x "$(command -v kind)" ]; then
    echo "Installing kind... https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-$(uname)-$GOARCH"
    curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-$(uname)-$_GOARCH
    mv ./kind ./bin/
    chmod 755 ./bin/kind
else
    echo "No need to install kind, continue..."
fi

# Install KUTTL if needed
if ! [ -x "$(command -v kubectl-kuttl)" ]; then
    echo "Installing kubectl-kuttl..."
    curl -LO https://github.com/kudobuilder/kuttl/releases/download/v0.11.0/kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH}
    mv ./kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH} ./bin/kubect-kuttl
    ls ./bin/
    chmod +x ./bin/kubect-kuttl
else
    echo "No need to install kubectl-kuttl, continue..."
fi

echo "Installing ginkgo and gomega..."
go get github.com/onsi/ginkgo/ginkgo
go get github.com/onsi/gomega/...
