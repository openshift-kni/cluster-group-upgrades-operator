#!/bin/bash

ENGINE=${ENGINE:-docker}

# Skip specific folders for now, until shellcheck warnings are addressed
find . -name '*.sh' -not -path './vendor/*' -not -path './git/*' \
    -not -path './pre-cache/*' -not -path './hack/*' -not -path './deploy/*' -print0 \
    | xargs -0 --no-run-if-empty ${ENGINE} run --rm -v "${PWD}:/mnt" docker.io/koalaman/shellcheck:v0.7.2
