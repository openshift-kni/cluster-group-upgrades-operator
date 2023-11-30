#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

dir_name=$(basename $PWD)
if [ $dir_name != "cluster-group-upgrades-operator" ]; then
    echo "This script must be run from root directory of the repo"
    echo "./hack/fix-path-structure.sh"
    exit 1
fi

parent_path=$(dirname $PWD)
parent=$(basename ${parent_path})
grandparent_path=$(dirname $parent_path)
grandparent=$(basename ${grandparent_path})

if [ $parent = "openshift-kni" ] && [ $grandparent = "github.com" ]; then
    echo "path is correct"
    exit 0
fi

cd ..
mkdir -p github.com/openshift-kni/
cp -r cluster-group-upgrades-operator github.com/openshift-kni/
cd github.com/openshift-kni/cluster-group-upgrades-operator/
echo "fixed path. pwd: $(pwd)"
$SHELL
