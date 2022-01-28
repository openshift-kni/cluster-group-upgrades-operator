# OCP upgrade image pre-cache #
This directory contains image pre-caching sample overrides configurations.
Overrides may be required to force pre-cache workload or payload image configurations.
Supported  overrides:
1. `precache.image` - pre-caching workload image pull spec. Normally derived from the operator csv.
2. `platform.image` - OCP release image 
3. `operators.indexes` - OLM index images (list of image pull specs)
4. `operators.packagesAndChannels` - operator packages and channels (list of  <package:channel> string entries)

:warning: We need to set the precache.image value as a container image digest. This value is going to be used by the pre-cache task to pull all the required container images and for the upgrade operator to replace the Clusterversion spec.desiredUpdate.image field. The cluster-version operator of the managed cluster requires this format to continue automatically with the upgrade since container image digest uniquely and immutably identifies a container image

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
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-group-upgrade-overrides
data:
  precache.image: quay.io/test_images/pre-cache:latest
  platform.image: quay.io/openshift-release-dev/ocp-release@sha256:3d5800990dee7cd4727d3fe238a97e2d2976d3808fc925ada29c559a47e2e1ef
  operators.indexes: |
    registry.example.com:5000/custom-redhat-operators:1.0.0
  operators.packagesAndChannels: |
    local-storage-operator: stable
    performance-addon-operator: 4.9
    ptp-operator: stable
    sriov-network-operator: stable
```

