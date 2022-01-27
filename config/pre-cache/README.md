# OCP upgrade image pre-cache #
This directory contains image pre-caching sample overrides configurations.
Overrides may be required to force pre-cache workload or payload image configurations.
Supported  overrides:
1. `precache.image` - pre-caching workload image pull spec. Normally derived from the operator csv.
2. `platform.image` - OCP release image 
3. `operators.indexes` - OLM index images (list of image pull specs)
4. `operators.packagesAndChannels` - operator packages and channels (list of  <package:channel> string entries)

Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-group-upgrade-overrides
data:
  precache.image: quay.io/test_images/pre-cache:latest
  platform.image: quay.io/openshift-release-dev/ocp-release:4.9.4-x86_64
  operators.indexes: |
    registry.example.com:5000/custom-redhat-operators:1.0.0
  operators.packagesAndChannels: |
    local-storage-operator: stable
    performance-addon-operator: 4.9
    ptp-operator: stable
    sriov-network-operator: stable
```

