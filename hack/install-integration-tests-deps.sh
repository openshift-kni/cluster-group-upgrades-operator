#!/bin/bash

set -e

_GOOS=$(go env GOOS)
_GOARCH=$(go env GOARCH)
_ARCH=$(arch)

KIND_ENV="kind"
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
    echo "Installing kind... https://github.com/kubernetes-sigs/kind/releases/download/v0.23.0/kind-$(uname)-$_GOARCH"
    curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/v0.23.0/kind-$(uname)-$_GOARCH
    mv ./kind ./bin/
    chmod 755 ./bin/kind
else
    echo "No need to install kind, continue..."
fi

# Install KUTTL if needed
if ! [ -x "$(command -v kubectl-kuttl)" ]; then
    echo "Installing kubectl-kuttl..."
    if ! curl -LOf https://github.com/kudobuilder/kuttl/releases/download/v0.11.0/kubectl-kuttl_0.11.0_${_GOOS}_${_GOARCH}; then
        if ! curl -LOf https://github.com/kudobuilder/kuttl/releases/download/v0.11.0/kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH}; then
            echo "Failed to download kubectl-kuttl binary for ${_GOARCH} or ${_ARCH}"
            exit 1
        else
            mv ./kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH} ./bin/kubectl-kuttl
        fi
    else
        mv ./kubectl-kuttl_0.11.0_${_GOOS}_${_GOARCH} ./bin/kubectl-kuttl
    fi
    ls ./bin/
    chmod +x ./bin/kubectl-kuttl
else
    echo "No need to install kubectl-kuttl, continue..."
fi

echo "Installing ginkgo and gomega..."
go get github.com/onsi/ginkgo/v2
go get github.com/onsi/gomega/...