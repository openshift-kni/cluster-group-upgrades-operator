# OCP upgrade image pre-cache #
This directory contains image pre-caching example reference-configurations.
Overrides may be required to force pre-cache workload or payload image configurations.
Supported  pre-caching configurations:
1. `overrides.preCacheImage` - pre-caching workload image pull spec. Normally derived from the operator csv.
2. `overrides.platformImage` - OCP release image 
3. `overrides.operatorsIndexes` - OLM index images (list of image pull specs)
4. `overrides.operatorsPackagesAndChannels` - operator packages and channels (list of  <package:channel> string entries)
5. `excludePrecachePatterns` - list of patterns to exclude from precaching (using this command: grep -vG -f)
6. `additionalImages` - list of additional images to be pre-cached
7. `spaceRequired` - disk space required for the pre-cached images

:warning: We need to set the `overrides.platformImage` value as a container image digest. This value is going to be used by the pre-cache task to pull all the required container images and for the upgrade operator to replace the Clusterversion `spec.desiredUpdate.image` field. The cluster-version operator of the managed cluster requires this format to continue automatically with the upgrade since container image digest uniquely and immutably identifies a container image

An example how to extract the container image digest from an ocp-release image using the oc binary:

```sh
$ oc adm release info 4.9.4
Name:      4.9.4
Digest:    sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e1ef
Created:   2021-10-25T23:18:37Z
OS/Arch:   linux/amd64
Manifests: 519

Pull From: quay.io/openshift-release-dev/ocp-release@sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e1ef
```


Example:

```yaml
apiVersion: ran.openshift.io/v1alpha1
kind: PreCachingConfig
metadata:
  name: example-config
  namespace: example-ns
spec:
  overrides:
    preCacheImage: quay.io/test_images/pre-cache:latest
    platformImage: quay.io/openshift-release-dev/ocp-release@sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e
    operatorsIndexes:
      - registry.example.com:5000/custom-redhat-operators:1.0.0
    operatorsPackagesAndChannels:
      - local-storage-operator: stable
      - ptp-operator: stable
      - sriov-network-operator: stable
  excludePrecachePatterns:
    - aws
    - vsphere
  additionalImages:
    - quay.io/foobar/application1@sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e
    - quay.io/foobar/application2@sha256:3d5800123dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47adf
    - quay.io/foobar/applicationN@sha256:4fe1334adfafadsf987123adfffdaf1243340adfafdedga0991234afdadfs
  spaceRequired: 45 GiB
```

