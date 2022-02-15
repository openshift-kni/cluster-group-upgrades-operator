#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$1/policies/policy5-subscriptions/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"NonCompliant"}, {"clustername":"spoke2","clusternamespace":"spoke2","compliant":"NonCompliant"}, {"clustername":"spoke5","clusternamespace":"spoke5","compliant":"NonCompliant"}]}}'
