#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/ran.openshift.io/v1alpha1/namespaces/$1/clustergroupupgrades/$2/status \
--data '{"status": {"conditions":[{"lastTransitionTime": "2021-12-15T18:55:59Z", "message": "All the clusters in the CR are compliant", "reason": "UpgradeCompleted", "status": "True", "type": "Succeeded"}]}}'