# Cluster Group Upgrades operator

## What is

Cluster Group Upgrades operator is a Kubernetes operator that facilitates software lifecycle management of fleets of clusters. It uses Red Hat Advanced Cluster Management (RHACM) for performing changes on target clusters, in particular by using RHACM policies.
Cluster Group Upgrades operator uses the following CRDs:

* ClusterGroupUpgrade

A ClusterGroupUpgrade CR defines a desired upgrade to a group clusters.
The spec allows you to define:
* Clusters belonging to group
* Number of concurrent upgrades
* Canaries
* Desired OpenShift version
* Desired operators versions

A set of example CRs can be found on the *samples* folder.

## How to deploy

1. Run **make docker-build docker-push IMG=*your_repo_image***
2. Run **make deploy IMG=*your_repo_image***

## How to test

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **cd integration_tests/*scenario***
3. Run **test.sh**

## How to develop

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **make install run**
