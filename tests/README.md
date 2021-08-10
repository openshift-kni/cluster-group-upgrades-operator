# Testing the Upgrades Operator

## Using kuttl

Prerequisites:
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [kubectl-kuttl](https://kuttl.dev/docs/#install-kuttl-cli)

```bash
 make kind-bootstrap-cluster
 make deploy
 make kuttl-test
```

or simply

```
make complete-kuttl-test
```

For development, a more suited approach is:
```bash
 make kind-bootstrap-cluster
 make install run
 make kuttl-test
```

<!---
Date: August/10/2021
-->

