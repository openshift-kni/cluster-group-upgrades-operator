#!/bin/bash
cd "$(dirname "$0")" || exit

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/work.open-cluster-management.io/v1/namespaces/$1/manifestworks/$2/status \
--data @$3
