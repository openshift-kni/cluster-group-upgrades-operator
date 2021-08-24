# Testing the Upgrades Operator

## Using kuttl

Prerequisites:
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [kubectl-kuttl](https://kuttl.dev/docs/#install-kuttl-cli)

```bash
 make kind-bootstrap-cluster
 make docker-build
 make deploy
 make kuttl-test
```

or simply

```
make complete-kuttl-test
```

For un-deploying the Upgrades operator:
```bash
make undeploy
```

For development, a more suited approach is:
```bash
 make kind-bootstrap-cluster
 make install run
 make kuttl-test
```

For deleting the cluster:
```bash
make kind-delete-cluster
```
<!---
Date: August/10/2021
-->

