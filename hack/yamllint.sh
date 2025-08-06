#!/bin/bash

VENVDIR=$(mktemp --tmpdir -d venv.XXXXXX)
if [ $? -ne 0 ]; then
    echo "Failed to create working directory" >&2
    exit 1
fi

function cleanup {
    rm -rf ${VENVDIR}
}
trap cleanup EXIT

function fatal {
    echo "$*" >&1
    exit 1
}

python3 -m venv ${VENVDIR} || fatal "Could not setup virtualenv"
# shellcheck disable=SC1091
source ${VENVDIR}/bin/activate || fatal "Could not activate virtualenv"

pip install yamllint==1.35.1 || fatal "Installation of yamllint failed"

find . -regextype egrep -regex '.*ya{0,1}ml$' \
    -not -path './bin/*' \
    -not -path './git/*' \
    -not -path './telco5g-konflux/*' \
    -not -path './testbin/*' \
    -not -path './vendor/*' \
    -print0 \
    | xargs -0 --no-run-if-empty yamllint -c .yamllint.yaml
