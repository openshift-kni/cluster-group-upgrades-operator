# OCP upgrade image pre-cache #
This directory contains image pre-caching example reference-configurations.
Overrides may be used to set the operator packages and channels to pre-cache.
Supported  pre-caching configurations:
1. `overrides.operatorsPackagesAndChannels` - operator packages and channels (list of  <package:channel> string entries)
2. `excludePrecachePatterns` - list of patterns to exclude from precaching (using this command: grep -vG -f)
3. `additionalImages` - list of additional images to be pre-cached
4. `spaceRequired` - disk space required for the pre-cached images

The `overrides.preCacheImage`, `overrides.platformImage`, and `overrides.operatorsIndexes` fields are deprecated and ignored by the controller.

Example:

```yaml
apiVersion: ran.openshift.io/v1alpha1
kind: PreCachingConfig
metadata:
  name: example-config
  namespace: example-ns
spec:
  overrides:
    operatorsPackagesAndChannels:
      - local-storage-operator:stable
      - ptp-operator:stable
      - sriov-network-operator:stable
  excludePrecachePatterns:
    - aws
    - vsphere
  additionalImages:
    - quay.io/foobar/application1@sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e
    - quay.io/foobar/application2@sha256:3d5800123dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47adf
    - quay.io/foobar/applicationN@sha256:4fe1334adfafadsf987123adfffdaf1243340adfafdedga0991234afdadfs
  spaceRequired: 45 GiB
```

