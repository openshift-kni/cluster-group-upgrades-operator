#!/bin/bash

# spoke1
curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/cgu-default-subscription-cluster-logging-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"cluster-logging","namespace":"openshift-logging"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa1","namespace":"openshift-logging","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa1"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/cgu-default-subscription-local-storage-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"openshift-local-storage","namespace":"openshift-local-storage"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa2","namespace":"openshift-local-storage","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa2"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/cgu-default-subscription-performance-addon-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"performance-addon-operator","namespace":"openshift-performance-addon-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa3","namespace":"openshift-performance-addon-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa3"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/cgu-default-subscription-ptp-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"ptp-operator-subscription","namespace":"openshift-ptp"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa4","namespace":"openshift-ptp","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa4"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/cgu-default-subscription-sriov-network-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"sriov-network-operator-subscription","namespace":"openshift-sriov-network-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa5","namespace":"openshift-sriov-network-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-aaaa5"}}}}}'


# spoke2
curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/cgu-default-subscription-cluster-logging-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"cluster-logging","namespace":"openshift-logging"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb1","namespace":"openshift-logging","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb1"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/cgu-default-subscription-local-storage-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"openshift-local-storage","namespace":"openshift-local-storage"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb2","namespace":"openshift-local-storage","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb2"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/cgu-default-subscription-performance-addon-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"performance-addon-operator","namespace":"openshift-performance-addon-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb3","namespace":"openshift-performance-addon-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb3"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/cgu-default-subscription-ptp-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"ptp-operator-subscription","namespace":"openshift-ptp"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb4","namespace":"openshift-ptp","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb4"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/cgu-default-subscription-sriov-network-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"sriov-network-operator-subscription","namespace":"openshift-sriov-network-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb5","namespace":"openshift-sriov-network-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-bbbb5"}}}}}'

# spoke5
curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/cgu-default-subscription-cluster-logging-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"cluster-logging","namespace":"openshift-logging"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc1","namespace":"openshift-logging","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc1"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/cgu-default-subscription-local-storage-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"openshift-local-storage","namespace":"openshift-local-storage"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc2","namespace":"openshift-local-storage","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc2"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/cgu-default-subscription-performance-addon-operator-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"performance-addon-operator","namespace":"openshift-performance-addon-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc3","namespace":"openshift-performance-addon-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc3"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/cgu-default-subscription-ptp-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"ptp-operator-subscription","namespace":"openshift-ptp"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc4","namespace":"openshift-ptp","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc4"}}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/cgu-default-subscription-sriov-network-operator-subscription-kuttl/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"Watching resources successfully", "reason":"GetResourceProcessing","status":"True","type":"Processing"}],"result":{"apiVersion":"apiVersion:operators.coreos.com\/v1alpha1","kind":"Subscription","metadata":{"name":"sriov-network-operator-subscription","namespace":"openshift-sriov-network-operator"},"status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc5","namespace":"openshift-sriov-network-operator","resourceVersion":"1528358"},"installplan":{"apiVersion":"operators.coreos.com\/v1alpha1","kind":"InstallPlan","name":"install-cccc5"}}}}}'
