#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/view-precache-spec-configmap/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/view-precache-spec-configmap/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/view-precache-spec-configmap/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke6/managedclusterviews/view-precache-spec-configmap/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/view-precache-service-acct/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/view-precache-service-acct/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/view-precache-service-acct/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke6/managedclusterviews/view-precache-service-acct/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/view-precache-cluster-role-binding/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/view-precache-cluster-role-binding/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/view-precache-cluster-role-binding/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke6/managedclusterviews/view-precache-cluster-role-binding/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"found","reason":"GetResourceProcessing","status":"True","type":"Processing"}]}}'