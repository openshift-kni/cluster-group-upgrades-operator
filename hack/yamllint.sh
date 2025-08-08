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

# yamllint >1.28 is not compatible with python 3.6 used in this release
pip install yamllint==1.28.0 || fatal "Installation of yamllint failed"

# File selection for yamllint is done in .yamllint.yaml
yamllint -c .yamllint.yaml .
