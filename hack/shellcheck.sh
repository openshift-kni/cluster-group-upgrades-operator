#!/bin/bash

shellcheck=$(go env GOPATH)/bin/shellcheck
if [ ! -f ${shellcheck} ]; then
    echo "Downloading shellcheck tool"
    scversion=v0.7.2
    arch=$(uname -m)
    wget -qO- "https://github.com/koalaman/shellcheck/releases/download/${scversion}/shellcheck-${scversion}.linux.${arch}.tar.xz" \
        | tar -xJ -C $(go env GOPATH)/bin --strip=1 shellcheck-${scversion}/shellcheck
fi

find . -name '*.sh' -not -path './vendor/*' -not -path './git/*' \
    -not -path './bin/*' -not -path './testbin/*' -print0 \
    | xargs -0 --no-run-if-empty ${shellcheck} -x
