#!/bin/bash

# Script to add a busybox sidecar container with shared volume to a Kubernetes deployment
# Also enables process namespace sharing (shareProcessNamespace: true) for debugging
# Usage: ./add-sidecar.sh <deployment-name> <namespace> <volume-name> <mount-path>

set -e

DEPLOYMENT_NAME="${1:-cluster-group-upgrades-controller-manager-v2}"
NAMESPACE="${2:-openshift-cluster-group-upgrades}"
VOLUME_NAME="${3:-coverage}"
MOUNT_PATH="${4:-/coverage}"

if [ -z "$DEPLOYMENT_NAME" ]; then
    echo "Usage: $0 <deployment-name> [namespace] [volume-name] [mount-path]"
    echo ""
    echo "Arguments:"
    echo "  deployment-name  Name of the deployment to patch"
    echo "  namespace        Kubernetes namespace"
    echo "  volume-name      Name of the shared volume"
    echo "  mount-path       Mount path for the shared volume"
    echo ""
    echo "Example:"
    echo "  $0 controller-manager-v2 openshift-cluster-group-upgrades coverage /coverage"
    exit 1
fi

echo "Adding busybox sidecar to deployment: $DEPLOYMENT_NAME"
echo "Namespace: $NAMESPACE"
echo "Volume name: $VOLUME_NAME"
echo "Mount path: $MOUNT_PATH"
echo ""

# Check if deployment exists
if ! oc get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE"; then
    echo "Error: Deployment '$DEPLOYMENT_NAME' not found in namespace '$NAMESPACE'"
    exit 1
fi

echo "Applying patch to add sidecar container..."

# Get the current deployment spec
CURRENT_SPEC=$(oc get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" -o json)

# Check if sidecar already exists
if echo "$CURRENT_SPEC" | jq -e '.spec.template.spec.containers[]? | select(.name == "sidecar-busybox")' &>/dev/null; then
    echo "Sidecar container already exists, skipping"
    exit 0
fi

# Check and enable shareProcessNamespace
if echo "$CURRENT_SPEC" | jq -e '.spec.template.spec.shareProcessNamespace == true' &>/dev/null; then
    echo "Process namespace sharing already enabled"
else
    echo "Enabling process namespace sharing..."
    oc patch deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --type=json -p='[
        {
            "op": "add",
            "path": "/spec/template/spec/shareProcessNamespace",
            "value": true
        }
    ]'
fi

# Add the sidecar container
echo "Adding busybox sidecar container..."
oc patch deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --type=json -p="[
    {
        \"op\": \"add\",
        \"path\": \"/spec/template/spec/containers/-\",
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
                \"allowPrivilegeEscalation\": false,
            }
        }
    }
]"

echo ""
echo "Sidecar added successfully!"
echo ""
echo "Waiting for rollout to complete..."
oc rollout status deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE"

echo ""
echo "Waiting for sidecar container to be ready..."

sleep 60

echo ""
echo "Deployment updated successfully!"
echo ""
echo "Features enabled:"
echo "  - Busybox sidecar container"
echo "  - Shared volume: $VOLUME_NAME mounted at $MOUNT_PATH"
echo "  - Process namespace sharing (shareProcessNamespace: true)"
echo ""
echo "To access the sidecar:"
echo "  oc exec -it -n $NAMESPACE deployment/$DEPLOYMENT_NAME -c sidecar-busybox -- sh"
echo ""
