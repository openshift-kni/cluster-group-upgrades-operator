#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$1/policies/policy1-common-cluster-version-policy/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"Compliant"}, {"clustername":"spoke4","clusternamespace":"spoke4","compliant":"NonCompliant"}, {"clustername":"spoke6","clusternamespace":"spoke6","compliant":"NonCompliant"}]}}'
