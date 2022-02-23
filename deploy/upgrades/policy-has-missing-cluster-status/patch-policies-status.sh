#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$1/policies/policy2-common-pao-sub-policy/status \
--data '{"status":{"status":[{"clustername":"spoke1","clusternamespace":"spoke1"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$2/policies/policy3-common-ptp-sub-policy/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke2","clusternamespace":"spoke2","compliant":"NonCompliant"}, {"clustername":"spoke4","clusternamespace":"spoke4","compliant":"NonCompliant"}]}}'
