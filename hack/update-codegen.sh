#!/bin/bash

# more info: https://cloud.redhat.com/blog/kubernetes-deep-dive-code-generation-customresources

set -o errexit
set -o nounset
set -o pipefail
export GOFLAGS="-mod=vendor"

cleanup() {
    echo "cleaning up..."
    rm -rf ./github.com/
}
trap "cleanup" EXIT

mkdir -p ./github.com/openshift-kni/cluster-group-upgrades-operator/api/clustergroupupgradesoperator
cp -r api/v1alpha1/ ./github.com/openshift-kni/cluster-group-upgrades-operator/api/clustergroupupgradesoperator/

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --input-pkg-root "github.com/openshift-kni/cluster-group-upgrades-operator/api" \
    --output-pkg-root "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/generated" \
    --output-base "./" \
    --boilerplate "./hack/boilerplate.go.txt"

rsync -ac --no-t --no-perms "./github.com/openshift-kni/cluster-group-upgrades-operator/pkg/" "./pkg/"

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
$SED -i "s|github.com/openshift-kni/cluster-group-upgrades-operator/github.com/openshift-kni/cluster-group-upgrades-operator/api/clustergroupupgradesoperator/v1alpha1|github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1|g" $(find pkg/generated -type f)
