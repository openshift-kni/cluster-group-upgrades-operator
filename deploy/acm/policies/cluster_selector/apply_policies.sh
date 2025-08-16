#!/bin/bash

oc apply -f ../all_policies/policy1-common-cluster-version-policy.yaml
oc apply -f ../all_policies/policy2-common-pao-sub-policy.yaml
oc apply -f ../all_policies/policy3-common-ptp-sub-policy.yaml
oc apply -f ../all_policies/policy4-common-sriov-sub-policy.yaml

# Patch the inform policies to reflect the compliance status.
../patch-policies-status.sh default default default default

# Create all the child policies to map the inform policies above.
oc apply --namespace=spoke1 -f ../all_policies/child-policy1-common-cluster-version-policy.yaml
oc apply --namespace=spoke4 -f ../all_policies/child-policy1-common-cluster-version-policy.yaml
oc apply --namespace=spoke6 -f ../all_policies/child-policy1-common-cluster-version-policy.yaml


oc apply --namespace=spoke1 -f ../all_policies/child-policy2-common-pao-sub-policy.yaml
oc apply --namespace=spoke2 -f ../all_policies/child-policy2-common-pao-sub-policy.yaml
oc apply --namespace=spoke4 -f ../all_policies/child-policy2-common-pao-sub-policy.yaml


oc apply --namespace=spoke2 -f ../all_policies/child-policy3-common-ptp-sub-policy.yaml
oc apply --namespace=spoke4 -f ../all_policies/child-policy3-common-ptp-sub-policy.yaml


oc apply --namespace=spoke4 -f ../all_policies/child-policy4-common-sriov-sub-policy.yaml
oc apply --namespace=spoke5 -f ../all_policies/child-policy4-common-sriov-sub-policy.yaml
oc apply --namespace=spoke6 -f ../all_policies/child-policy4-common-sriov-sub-policy.yaml
