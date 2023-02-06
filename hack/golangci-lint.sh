#!/bin/bash

which golangci-lint
if [ $? -ne 0 ]; then
    echo "Downloading golangci-lint tool"
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -d -b $(go env GOPATH)/bin v1.51.1

    if [ $? -ne 0 ]; then
        echo "Install from script failed. Trying go install"
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.51.1
        if [ $? -ne 0 ]; then
            echo "Install of golangci-lint failed"
            exit 1
        fi
    fi
fi

export GOCACHE=/tmp/
export GOLANGCI_LINT_CACHE=/tmp/.cache
golangci-lint run --verbose --print-resources-usage --modules-download-mode=vendor --timeout=5m0s
