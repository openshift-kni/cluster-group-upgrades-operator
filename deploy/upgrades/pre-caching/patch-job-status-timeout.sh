#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke1/managedclusterviews/view-precache-job/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","reason":"GetResourceProcessing","message":"found","status":"True","type":"Processing"}],"result":{"status":{"active":0,"succeeded":1}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke2/managedclusterviews/view-precache-job/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","reason":"GetResourceProcessing","message":"found","status":"True","type":"Processing"}],"result":{"status":{"active":0,"succeeded":0,"conditions": [{"lastTransitionTime":"2022-01-28T17:57:00Z","message":"timedout","type":"Failed","status":"True","reason":"DeadlineExceeded"}]}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke5/managedclusterviews/view-precache-job/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","reason":"GetResourceProcessing","message":"found","status":"True","type":"Processing"}],"result":{"status":{"active":1,"succeeded":0}}}}'

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/view.open-cluster-management.io/v1beta1/namespaces/spoke6/managedclusterviews/view-precache-job/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","reason":"GetResourceProcessing","message":"found","status":"True","type":"Processing"}],"result":{"status":{"active":0,"succeeded":1}}}}'
