#!/bin/bash
cd "$(dirname "$0")" || exit

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/json-patch+json" \
http://localhost:8001/apis/lcm.openshift.io/v1alpha1/namespaces/$1/imagebasedgroupupgrades/$2 \
--data @$3
