#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$1/policies/policy0-common-config-policy/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"Compliant"}, {"clustername":"spoke2","clusternamespace":"spoke2","compliant":"Compliant"}, {"clustername":"spoke5","clusternamespace":"spoke5","compliant":"Compliant"}, {"clustername":"spoke6","clusternamespace":"spoke6","compliant":"Compliant"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$2/policies/policy2-common-pao-sub-policy/status \
--data '{"status":{"compliant":"Compliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"Compliant"}, {"clustername":"spoke2","clusternamespace":"spoke2","compliant":"Compliant"}, {"clustername":"spoke5","clusternamespace":"spoke5","compliant":"Compliant"}, {"clustername":"spoke6","clusternamespace":"spoke6","compliant":"Compliant"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$3/policies/policy3-common-ptp-sub-policy/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"Compliant"}, {"clustername":"spoke2","clusternamespace":"spoke2","compliant":"NonCompliant"}, {"clustername":"spoke4","clusternamespace":"spoke4","compliant":"NonCompliant"}, {"clustername":"spoke5","clusternamespace":"spoke5","compliant":"Compliant"}, {"clustername":"spoke6","clusternamespace":"spoke6","compliant":"Compliant"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/policy.open-cluster-management.io/v1/namespaces/$4/policies/policy4-common-sriov-sub-policy/status \
--data '{"status":{"compliant":"NonCompliant","status":[{"clustername":"spoke1","clusternamespace":"spoke1","compliant":"Compliant"}, {"clustername":"spoke4","clusternamespace":"spoke4","compliant":"NonCompliant"}, {"clustername":"spoke5","clusternamespace":"spoke5","compliant":"Compliant"}, {"clustername":"spoke6","clusternamespace":"spoke6","compliant":"Compliant"}]}}'

