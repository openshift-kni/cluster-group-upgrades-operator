#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

module="github.com/openshift-kni/cluster-group-upgrades-operator"
if [ ! -d $(dirname "${BASH_SOURCE[0]}")/../../../../${module} ]; then
    echo "In order to use this script the path structure should be:"
    echo $module
    echo "and it should be run as: ./hack/update-codegen.sh"
    exit 1
fi

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# shellcheck disable=SC1091
source "${CODEGEN_PKG}/kube_codegen.sh"


kube::codegen::gen_helpers \
    --input-pkg-root ${module}/pkg/api \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

kube::codegen::gen_client \
    --with-applyconfig \
    --with-watch \
    --input-pkg-root ${module}/pkg/api \
    --output-pkg-root ${module}/pkg/generated \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"
