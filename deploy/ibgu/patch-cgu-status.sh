#!/bin/bash
cd "$(dirname "$0")" || exit

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/json-patch+json" \
http://localhost:8001/apis/ran.openshift.io/v1alpha1/namespaces/$1/clustergroupupgrades/$2/status \
--data @$3
