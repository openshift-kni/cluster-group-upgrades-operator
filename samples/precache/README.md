# Pre-caching manual test #
This directory contains samples for manual standalone pre-caching testing.
All the commands provided here must be executed from the main project directory with KUBECONFIG environment variable initialized and pointing to the ACM hub cluster.
## Mocks and workarounds $$
For running locally on a non-ztp deployed cluster, some things must be mocked. 
1. ZTP workflow creates kubeconfig admin secrets automatically. If the cluster under test is deployed differently, create kubeconfig secrets in the managed cluster namespaces on the hub, for example:
```bash
oc create secret generic spoke2-admin-kubeconfig -n spoke2 --from-file=kubeconfig=spoke2.kubeconfig
```
2. If the operator is managed by OLM, operator CSV is present on the hub and contains the precaching workload image pull spec. If testing locally, the CSV object must be mocked.Adjust the precaching image pull spec in [cguo-csv-sample.yaml](cguo-csv-sample.yaml#L253) to your desired location. Deploy mock policies, CRD, CSV and CGU by
```bash
while true; do oc apply -k samples/precache; [[ $? -ne 0 ]] || break; done
```
### Note on catalogsource policy ###
When working with a disconnected registry, cluster administrators will have to create such a policy for the cluster to function. For connected environments, spoke clusters are preconfigured with default catalog sources (such as redhat-operators), and no catalog source policy will be necessary. 
Precaching, however, requires a catalog source policy to be explicitly configured on the hub cluster. The example catalog source policy for working with default registry is provided in [catsrc-policy.yaml](catsrc-policy.yaml)

## Running the controller ##
```bash
make run
```