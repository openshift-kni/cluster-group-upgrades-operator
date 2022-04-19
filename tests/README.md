# Testing the Upgrades Operator
## Integration tests using a cluster running RHACM and the deployed ClusterGroupUpgrades operator

Export **KUBECONFIG** environment variable to point to your cluster running RHACM

```bash
 make complete-deployment
 make kuttl-test
 make stop-test-proxy
```

or simply

```
make complete-deployment
```

## Integration tests using kuttl and kind
Prerequisites:
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [kubectl-kuttl](https://kuttl.dev/docs/#install-kuttl-cli)

```bash
 make kind-deps-update
 make kind-bootstrap-cluster
 make docker-build
 make kind-load-operator-image
 make deploy
```

or simply

```
 make kind-complete-kuttl-test
```

For un-deploying the Upgrades operator:
```bash
 make undeploy
```

For development, a more suited approach is:
```bash
 make kind-deps-update
 make kind-bootstrap-cluster
 make install run
 make kuttl-test
```

For deleting the cluster:
```bash
 make kind-delete-cluster
```

# Unit tests

```bash
 make ci-job
```
<!---
Date: April/19/2022
-->
