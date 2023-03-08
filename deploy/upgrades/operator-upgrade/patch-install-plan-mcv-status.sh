#!/bin/bash

# spoke1
curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/install-aaaa1/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-aaaa1","namespace":"openshift-logging","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/install-aaaa2/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-aaaa2","namespace":"openshift-local-storage","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/install-aaaa3/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-aaaa3","namespace":"openshift-performance-addon-operator","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/install-aaaa4/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-aaaa4","namespace":"openshift-ptp","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/install-aaaa5/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-aaaa5","namespace":"openshift-sriov-network-operator","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'


# spoke2
curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/install-bbbb1/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-bbbb1","namespace":"openshift-logging","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/install-bbbb2/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-bbbb2","namespace":"openshift-local-storage","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/install-bbbb3/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-bbbb3","namespace":"openshift-performance-addon-operator","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/install-bbbb4/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-bbbb4","namespace":"openshift-ptp","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/install-bbbb5/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","metadata":{"name":"install-bbbb5","namespace":"openshift-sriov-network-operator","resourceVersion":"1532546"},"spec":{"approval":"Manual","approved":"false"},"status":{"phase":"RequiresApproval"}}}}'
