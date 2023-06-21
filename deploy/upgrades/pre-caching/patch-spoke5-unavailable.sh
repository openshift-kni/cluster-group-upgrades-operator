#!/bin/bash

curl -k -s -X PATCH -H "Accept: application/json, */*" \
-H "Content-Type: application/merge-patch+json" \
http://localhost:8001/apis/cluster.open-cluster-management.io/v1/managedclusters/spoke5/status \
--data '{"status":{"conditions":[{"lastTransitionTime":"2022-01-28T17:57:00Z","reason":"unreachable","message":"unreachable","status":"False","type":"ManagedClusterConditionAvailable"}]}}'
