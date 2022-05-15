#!/bin/bash

which golangci-lint
if [ $? -ne 0 ]; then
    echo "Downloading golangci-lint tool"
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.46.1
fi

export GOCACHE=/tmp/
export GOLANGCI_LINT_CACHE=/tmp/.cache
golangci-lint run --verbose --print-resources-usage --modules-download-mode=vendor --timeout=5m0s
