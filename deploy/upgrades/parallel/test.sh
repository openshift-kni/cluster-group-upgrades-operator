#!/bin/bash
set -e


error() {
    echo "Test at line $1 failed"
}
trap 'error $LINENO' ERR

echo "Creating resources"

oc apply -f common.yaml
oc apply -f site1.yaml
oc apply -f site2.yaml
oc apply -f group1.yaml

echo "Sleeping a bit"
sleep 5

echo "Testing placement rules generated"
diff site1-placement-rule.spec <(oc get placementrules.apps.open-cluster-management.io site1 -ojson | jq -Sc '.spec') 
diff site2-placement-rule.spec <(oc get placementrules.apps.open-cluster-management.io site2 -ojson | jq -Sc '.spec') 
diff group1-batch-1-placement-rule.spec <(oc get placementrules.apps.open-cluster-management.io group1-batch-1 -ojson | jq -Sc '.spec')

echo "Testing placement bindings generated"
diff site1-placement-binding.placementRef <(oc get placementbindings.policy.open-cluster-management.io site1 -ojson | jq -Sc '.placementRef') 
diff site1-placement-binding.subjects <(oc get placementbindings.policy.open-cluster-management.io site1 -ojson | jq -Sc '.subjects') 
diff site2-placement-binding.placementRef <(oc get placementbindings.policy.open-cluster-management.io site2 -ojson | jq -Sc '.placementRef') 
diff site2-placement-binding.subjects <(oc get placementbindings.policy.open-cluster-management.io site2 -ojson | jq -Sc '.subjects') 
diff group1-batch-1-placement-binding.placementRef <(oc get placementbindings.policy.open-cluster-management.io group1-batch-1 -ojson | jq -Sc '.placementRef') 
diff group1-batch-1-placement-binding.subjects <(oc get placementbindings.policy.open-cluster-management.io group1-batch-1 -ojson | jq -Sc '.subjects') 

echo "Testing policies generated"
diff common-group1-batch-1-policy.spec <(oc get policies.policy.open-cluster-management.io common-group1-batch-1-common-namespace -ojson | jq -Sc '.spec')
diff site1-policy.spec <(oc get policies.policy.open-cluster-management.io site1-site1-namespace -ojson | jq -Sc '.spec') 
diff site2-policy.spec <(oc get policies.policy.open-cluster-management.io site2-site2-namespace -ojson | jq -Sc '.spec') 
diff group1-batch-1-policy.spec <(oc get policies.policy.open-cluster-management.io group1-batch-1-group1-namespace -ojson | jq -Sc '.spec')

echo "Deleting resources"
oc delete -f common.yaml
oc delete -f site1.yaml
oc delete -f site2.yaml
oc delete -f group1.yaml
