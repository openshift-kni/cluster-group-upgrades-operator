#!/bin/bash

VERSION="0.10.0"

rootdir=$(git rev-parse --show-toplevel)
if [ -z "${rootdir}" ]; then
    echo "Failed to determine top level directory"
    exit 1
fi

bindir="${rootdir}/bin"
shellcheck="${bindir}/shellcheck"

function get_os {
    local os
    # On github shellcheck uses lowercase names in its release url
    os=$(uname | awk '{print tolower($0)}')
    echo "${os}"
}

function get_arch {
    local arch
    arch=$(uname -m)

    local os
    os=$(get_os)

    # MacOS returns arm64, but shellcheck uses aarch64
    if [[ $os == 'darwin' ]]; then
        if [[ $arch == 'arm64' ]]; then
            arch='aarch64'
        fi
    fi

    echo "${arch}"
}

function get_tool {
    mkdir -p "${bindir}"
    echo "Downloading shellcheck tool"
    scversion="v${VERSION}"
    wget -qO- "https://github.com/koalaman/shellcheck/releases/download/${scversion}/shellcheck-${scversion}.$(get_os).$(get_arch).tar.xz" \
        | tar -xJ -C "${bindir}" --strip=1 shellcheck-${scversion}/shellcheck
}

if [ ! -f ${shellcheck} ]; then
    get_tool
else
    current_ver=$("${shellcheck}" --version | grep '^version:' | awk '{print $2}')
    if [ "${current_ver}" != "${VERSION}" ]; then
        get_tool
    fi
fi

find . -name '*.sh' -not -path './vendor/*' -not -path './*/vendor/*' -not -path './git/*' \
    -not -path './bin/*' -not -path './testbin/*' -print0 \
    | xargs -0 --no-run-if-empty ${shellcheck} -x
