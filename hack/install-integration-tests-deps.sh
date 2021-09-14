#!/bin/bash

set -e

_GOOS=$(go env GOOS)
_GOARCH=$(go env GOARCH)
_ARCH=$(arch)

mkdir -p ./bin

# Install kubectl if needed.
if ! [ -x "$(command -v kubectl)" ]; then
    echo "Installing kubectl..."
    curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/$_GOOS/$_GOARCH/kubectl
    mv ./kubectl ./bin/
    chmod 755 ./bin/kubectl
    sudo mv ./bin/kubectl /usr/local/bin/
else
    echo "kubectl present, continue..."
fi

# Install kind if needed.
if ! [ -x "$(command -v kind)" ]; then
    echo "Installing kind... https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-$(uname)-$GOARCH"
    curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-$(uname)-$_GOARCH
    mv ./kind ./bin/
    chmod 755 ./bin/kind
    sudo mv ./bin/kind /usr/local/bin/kind
else
    echo "kind present, continue..."
fi

# Install KUTTL if needed
if ! [ -x "$(command -v kind)" ]; then
    echo "Installing kubectl-kuttl..."
    curl -LO https://github.com/kudobuilder/kuttl/releases/download/v0.11.0/kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH}
    mv ./kubectl-kuttl_0.11.0_${_GOOS}_${_ARCH} ./bin/kubect-kuttl
    ls ./bin/
    chmod +x ./bin/kubect-kuttl
    sudo mv ./bin/kubectl-kuttl /usr/local/bin/kubectl-kuttl
else
    echo "kubectl-kuttl present, continue..."
fi

echo "Installing ginkgo and gomega..."
go get github.com/onsi/ginkgo/ginkgo
go get github.com/onsi/gomega/...
