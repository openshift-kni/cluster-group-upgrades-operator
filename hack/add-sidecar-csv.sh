#!/bin/bash

# Script to add a busybox sidecar container with shared volume to the deployment inside the running CSV
# Also enables process namespace sharing (shareProcessNamespace: true) for debugging
# Usage: ./add-sidecar-csv.sh [csv-name] [namespace] [volume-name] [mount-path]

set -e

NAMESPACE="${1:-openshift-cluster-group-upgrades}"
CSV_NAME="${2}"
VOLUME_NAME="${3:-coverage}"
MOUNT_PATH="${4:-/coverage}"

if [ $NAMESPACE = "openshift-cluster-group-upgrades" ]; then
    LABEL="operators.coreos.com/cluster-group-upgrades-operator.openshift-cluster-group-upgrade"
else
    LABEL="operators.coreos.com/cluster-group-upgrades-operator.$NAMESPACE"
fi

# Auto-detect CSV name if not provided
if [ -z "$CSV_NAME" ]; then
    echo "No CSV name provided, auto-detecting using label selector..."
    CSV_NAME=$(oc get csv -n "$NAMESPACE" -l "$LABEL" -o jsonpath='{.items[0].metadata.name}' || echo "")

    if [ -z "$CSV_NAME" ]; then
        echo "Error: Could not find CSV with label 'operators.coreos.com/cluster-group-upgrades-operator.openshift-cluster-group-upgrade' in namespace '$NAMESPACE'"
        echo ""
        echo "Available CSVs in namespace $NAMESPACE:"
        oc get csv -n "$NAMESPACE" -o name
        echo ""
        echo "Usage: $0 [namespace] [csv-name]  [volume-name] [mount-path]"
        echo ""
        echo "Arguments:"
        echo "  namespace        Kubernetes namespace (default: clusters)"
        echo "  csv-name         Name of the ClusterServiceVersion (auto-detected if not provided)"
        echo "  volume-name      Name of the shared volume (default: coverage)"
        echo "  mount-path       Mount path for the shared volume (default: /coverage)"
        echo ""
        echo "Example:"
        echo "  $0 openshift-cluster-group-upgrades cluster-group-upgrades-operator.v4.21.0 coverage /coverage"
        exit 1
    fi
    echo "Auto-detected CSV: $CSV_NAME"
fi

echo "Adding busybox sidecar to CSV deployment spec: $CSV_NAME"
echo "Namespace: $NAMESPACE"
echo "Volume name: $VOLUME_NAME"
echo "Mount path: $MOUNT_PATH"
echo ""

# Check if CSV exists
if ! oc get csv "$CSV_NAME" -n "$NAMESPACE" ; then
    echo "Error: ClusterServiceVersion '$CSV_NAME' not found in namespace '$NAMESPACE'"
    echo ""
    echo "Available CSVs in namespace $NAMESPACE:"
    oc get csv -n "$NAMESPACE" -o name
    exit 1
fi

echo "Found CSV: $CSV_NAME"

# Get the current CSV spec
CURRENT_SPEC=$(oc get csv "$CSV_NAME" -n "$NAMESPACE" -o json)

# The deployment is typically at index 0 in .spec.install.spec.deployments[]
# Check if sidecar already exists
if echo "$CURRENT_SPEC" | jq -e '.spec.install.spec.deployments[0].spec.template.spec.containers[]? | select(.name == "sidecar-busybox")'; then
    echo "Sidecar container already exists in CSV, skipping"
    exit 0
fi

# Check and enable shareProcessNamespace
if echo "$CURRENT_SPEC" | jq -e '.spec.install.spec.deployments[0].spec.template.spec.shareProcessNamespace == true'; then
    echo "Process namespace sharing already enabled in CSV"
else
    echo "Enabling process namespace sharing in CSV..."
    oc patch csv "$CSV_NAME" -n "$NAMESPACE" --type=json -p='[
        {
            "op": "add",
            "path": "/spec/install/spec/deployments/0/spec/template/spec/shareProcessNamespace",
            "value": true
        }
    ]'
fi

# Add the sidecar container
echo "Adding busybox sidecar container to CSV deployment spec..."
oc patch csv "$CSV_NAME" -n "$NAMESPACE" --type=json -p="[
    {
        \"op\": \"add\",
        \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/-\",
        \"value\": {
            \"name\": \"sidecar-busybox\",
            \"image\": \"busybox:latest\",
            \"command\": [\"sh\", \"-c\", \"while true; do sleep 3600; done\"],
            \"volumeMounts\": [
                {
                    \"name\": \"$VOLUME_NAME\",
                    \"mountPath\": \"$MOUNT_PATH\"
                }
            ],
            \"securityContext\": {
                \"allowPrivilegeEscalation\": false
            }
        }
    }
]"

echo ""
echo "Sidecar added successfully to CSV!"
echo ""
echo "CSV updated: $CSV_NAME"
echo ""
echo "Features enabled in deployment spec:"
echo "  - Busybox sidecar container"
echo "  - Shared volume: $VOLUME_NAME mounted at $MOUNT_PATH"
echo "  - Process namespace sharing (shareProcessNamespace: true)"
echo ""
echo "Note: OLM will reconcile the deployment based on the updated CSV."
echo "The deployment will be automatically updated by the OLM operator."
echo ""
