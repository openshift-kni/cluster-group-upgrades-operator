#!/bin/bash

# more info: https://cloud.redhat.com/blog/kubernetes-deep-dive-code-generation-customresources

set -o errexit
set -o nounset
set -o pipefail
export GOFLAGS="-mod=vendor"

cleanup() {
    echo "cleaning up..."
    rm -rf ./pkg/github.com/
    rm -rf api/clustergroupupgradesoperator
}
trap "cleanup" EXIT

mkdir -p ./api/clustergroupupgradesoperator
cd ./api/clustergroupupgradesoperator
ln -s ../v1alpha1 ./v1alpha1
cd ../../

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# TIP: append '-v 10' with the script call below for logs
bash ${CODEGEN_PKG}/generate-groups.sh "all" \
    "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/generated" \
    "github.com/openshift-kni/cluster-group-upgrades-operator/api" \
    clustergroupupgradesoperator:v1alpha1 \
    --output-base "./pkg" \
    --go-header-file hack/boilerplate.go.txt

rsync -ac --no-t --no-perms "./pkg/github.com/openshift-kni/cluster-group-upgrades-operator/pkg/" "./pkg/"

# linux and macos support
SED="sed"
unamestr=$(uname)
if [[ "$unamestr" == "Darwin" ]] ; then
    SED="gsed"
    type $SED >/dev/null 2>&1 || {
        echo >&2 "$SED it's not installed. Try: brew install gnu-sed" ;
        exit 1;
    }
fi
$SED -i "s|api/clustergroupupgradesoperator/v1alpha1|api/v1alpha1|g" $(find pkg/generated -type f)
