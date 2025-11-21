#!/bin/bash

# Script to extract coverage data from running operator deployment
# This script:
#   1. Adds a busybox sidecar with shared process namespace
#   2. Sends SIGINT to the manager process to flush coverage data
#   3. Copies coverage data from the pod and generates reports

set -e

NAMESPACE="${1:-openshift-cluster-group-upgrades}"
DEPLOYMENT="${2:-cluster-group-upgrades-controller-manager-v2}"
OUTPUT_DIR="${3:-./runtime-coverage}"
COVERAGE_PATH="${4:-/coverage}"
VOLUME_NAME="${5:-coverage}"
FLUSH_DELAY="${6:-5}"  # Seconds to wait after SIGINT for coverage flush

# Get the script directory to find add-sidecar.sh
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
    echo "Usage: $0 [namespace] [deployment] [output-dir] [coverage-path] [volume-name] [flush-delay]"
    echo ""
    echo "Arguments:"
    echo "  namespace      Kubernetes namespace (default: openshift-cluster-group-upgrades)"
    echo "  deployment     Deployment name (default: controller-manager-v2)"
    echo "  output-dir     Local directory for coverage data (default: ./runtime-coverage)"
    echo "  coverage-path  Coverage path in container (default: /coverage)"
    echo "  volume-name    Name of coverage volume (default: coverage)"
    echo "  flush-delay    Seconds to wait after SIGINT (default: 10)"
    echo ""
    echo "Example:"
    echo "  $0 openshift-cluster-group-upgrades controller-manager-v2 ./coverage"
    echo ""
    echo "This script will:"
    echo "  1. Add a busybox sidecar with shareProcessNamespace enabled"
    echo "  2. Send SIGINT to /manager process to trigger coverage flush"
    echo "  3. Wait for coverage data to be written to disk"
    echo "  4. Copy coverage data and generate reports"
    exit 1
}

if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    usage
fi

echo "=========================================="
echo "Coverage Data Extraction Tool"
echo "=========================================="
echo "Namespace:     $NAMESPACE"
echo "Deployment:    $DEPLOYMENT"
echo "Output Dir:    $OUTPUT_DIR"
echo "Coverage Path: $COVERAGE_PATH"
echo "Volume Name:   $VOLUME_NAME"
echo "Flush Delay:   ${FLUSH_DELAY}s"
echo ""

# Check if deployment exists
echo "Step 1: Checking deployment..."
if ! oc get deployment "$DEPLOYMENT" -n "$NAMESPACE"; then
    echo "Error: Deployment '$DEPLOYMENT' not found in namespace '$NAMESPACE'"
    echo ""
    echo "Available deployments:"
    oc get deployments -n "$NAMESPACE" 2>/dev/null || echo "  Namespace not found or no deployments"
    exit 1
fi
echo "✓ Deployment found"

# Check if add-sidecar.sh exists
if [ ! -f "$SCRIPT_DIR/add-sidecar.sh" ]; then
    echo "Error: add-sidecar.sh not found at $SCRIPT_DIR/add-sidecar.sh"
    exit 1
fi

# Step 1: Add sidecar with shared process namespace
echo ""
echo "=========================================="
echo "Step 2: Adding Busybox Sidecar"
echo "=========================================="
echo ""
echo "This will add a sidecar container and enable shareProcessNamespace..."
echo ""

"$SCRIPT_DIR/add-sidecar.sh" "$DEPLOYMENT" "$NAMESPACE" "$VOLUME_NAME" "$COVERAGE_PATH"

if [ $? -ne 0 ]; then
    echo ""
    echo "Error: Failed to add sidecar"
    exit 1
fi

echo ""
echo "✓ Sidecar added successfully"

# Get the pod name
echo ""
echo "=========================================="
echo "Step 3: Finding Pod"
echo "=========================================="
POD_NAME=$(oc get pods -n "$NAMESPACE" -l control-plane=controller-manager --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$POD_NAME" ]; then
    echo "Error: No running pod found for deployment '$DEPLOYMENT'"
    echo ""
    echo "Pod status:"
    oc get pods -n "$NAMESPACE" -l control-plane=controller-manager
    exit 1
fi

echo "Found pod: $POD_NAME"

# Verify sidecar is running
echo ""
echo "Verifying sidecar container..."
SIDECAR_STATUS=$(oc get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[?(@.name=="sidecar-busybox")].ready}' 2>/dev/null)

if [ "$SIDECAR_STATUS" != "true" ]; then
    echo "sidecar is not ready"
    exit 1
fi

echo "✓ Sidecar is ready"

# Step 2: Find and signal the manager process
echo ""
echo "=========================================="
echo "Step 4: Triggering Coverage Flush"
echo "=========================================="
echo ""
echo "Finding /manager process via sidecar..."

MANAGER_PID=$(oc exec -n "$NAMESPACE" "$POD_NAME" -c sidecar-busybox -- sh -c "ps aux | grep '/manager' | grep -v grep | awk '{print \$1}'" 2>/dev/null | head -1)

if [ -z "$MANAGER_PID" ]; then
    echo "Error: Could not find /manager process"
    echo ""
    echo "All processes in pod:"
    oc exec -n "$NAMESPACE" "$POD_NAME" -c sidecar-busybox -- ps aux
    exit 1
fi

echo "Found /manager process with PID: $MANAGER_PID"
echo ""
echo "Sending SIGINT to trigger coverage flush..."

oc exec -n "$NAMESPACE" "$POD_NAME" -c sidecar-busybox -- kill -INT "$MANAGER_PID" 2>/dev/null

if [ $? -eq 0 ]; then
    echo "✓ SIGINT sent successfully"
else
    echo "Warning: Failed to send SIGINT (process may have already exited)"
fi

echo ""
echo "Waiting ${FLUSH_DELAY} seconds for coverage data to be written to disk..."
for _ in $(seq 1 "$FLUSH_DELAY"); do
    echo -n "."
    sleep 1
done
echo ""
echo "✓ Flush delay complete"

# Step 3: Copy coverage data
echo ""
echo "=========================================="
echo "Step 5: Copying Coverage Data"
echo "=========================================="
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Check if there are coverage files
echo "Checking for coverage files..."
FILE_COUNT=$(oc exec -n "$NAMESPACE" "$POD_NAME" -c sidecar-busybox -- sh -c "ls -1 $COVERAGE_PATH 2>/dev/null | wc -l" || echo "0")

if [ "$FILE_COUNT" -eq 0 ]; then
    echo "Warning: No coverage files found in $COVERAGE_PATH"
    echo ""
    echo "Directory contents:"
    oc exec -n "$NAMESPACE" "$POD_NAME" -c sidecar-busybox -- ls -la "$COVERAGE_PATH" || true
    echo ""
    echo "This could mean:"
    echo "  1. The operator was not built with -cover flag"
    echo "  2. GOCOVERDIR is not set correctly"
    echo "  3. The process hasn't generated coverage yet"
    exit 1
fi

echo "Found $FILE_COUNT coverage file(s)"

# Copy coverage data from sidecar
echo ""
echo "Copying coverage data from sidecar container..."
oc cp -n "$NAMESPACE" -c sidecar-busybox "$POD_NAME:$COVERAGE_PATH" "$OUTPUT_DIR" 2>&1 | grep -v "Defaulting container" || true

# Verify files were copied
COPIED_COUNT=$(find "$OUTPUT_DIR" -type f 2>/dev/null | wc -l)
if [ "$COPIED_COUNT" -eq 0 ]; then
    echo "Error: No files were copied from the pod"
    exit 1
fi

echo "✓ Copied $COPIED_COUNT file(s) to $OUTPUT_DIR"

# List the coverage files
echo ""
echo "Coverage files:"
ls -lh "$OUTPUT_DIR"

# Step 4: Generate coverage reports
echo ""
echo "=========================================="
echo "Step 6: Generating Coverage Reports"
echo "=========================================="

# Convert to text format
echo ""
echo "Converting coverage data to text format..."
if go tool covdata textfmt -i="$OUTPUT_DIR" -o="$OUTPUT_DIR/coverage.out" 2>&1; then
    echo "✓ Generated: $OUTPUT_DIR/coverage.out"
else
    echo "✗ Failed to generate coverage.out"
    echo "Make sure you have Go installed and the coverage data is valid"
    exit 1
fi

echo ""
echo "=========================================="
echo "Extraction Complete!"
echo "=========================================="
echo ""
echo "Output directory: $OUTPUT_DIR"
echo ""
echo "Generated files:"
echo "  - coverage.out           : Text format coverage data"
echo ""
