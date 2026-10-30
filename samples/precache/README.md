# Pre-caching manual test #
This directory contains samples for manual standalone pre-caching testing.
All the commands provided here must be executed from the main project directory with KUBECONFIG environment variable initialized and pointing to the ACM hub cluster.
## Mocks and workarounds ##
1. Deploy the CRD from [config/crd/bases/ran.openshift.io_clustergroupupgrades.yaml](../../config/crd/bases/ran.openshift.io_clustergroupupgrades.yaml)
1. Deploy the mock policies and objects by
```bash
oc apply -k samples/precache
```
### Note on catalogsource policy ###
When working with a disconnected registry, cluster administrators will have to create such a policy for the cluster to function. For connected environments, spoke clusters are preconfigured with default catalog sources (such as redhat-operators), and no catalog source policy will be necessary. 
Precaching, however, requires a catalog source policy to be explicitly configured on the hub cluster. The example catalog source policy for working with default registry is provided in [catsrc-parent-policy.yaml](catsrc-parent-policy.yaml) and [catsrc-child-policy.yaml](catsrc-child-policy.yaml)

## Running the controller ##
```bash
make run
```
To run with custom pre-caching images, add the corresponding environment variable, for example:
```bash
PRECACHE_IMG=<my registry>/<my repository>/<my image>:<tag> make run
```
