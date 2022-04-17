# Cluster Group Upgrades operator

## What is

Cluster Group Upgrades operator is a Kubernetes operator that facilitates software lifecycle management of fleets of clusters. It uses Red Hat Advanced Cluster Management (RHACM) for performing changes on target clusters, in particular by using RHACM policies.
Cluster Group Upgrades operator uses the following CRDs:

* **ClusterGroupUpgrade**

A ClusterGroupUpgrade CR defines a desired upgrade to a group clusters.
The spec allows you to define:
* Clusters belonging to group
* Number of concurrent upgrades
* Canaries
* Desired OpenShift version
* Desired operators versions

A set of example CRs can be found on the **samples** folder.

## How it works

The ClusterGroupUpgrade controller is designed to perform an upgrade to a group of clusters
in a given period of time, which by default is set to 4h.
In order to model the various phases of an upgrade, the ClusterGroupUpgrade CR can be in the
following states:

* **UpgradeNotStarted**
  * In this state, the **ClusterGroupUpgrade** CR has just been created and the *remediationAction* field is set to *inform*
  * The controller will build a remediation plan based on the *clusters* list and with *remediationStrategy* fields like:
    * If *canaries* field is defined with a list of clusters, the first batch of the remediation plan will contain those clusters
    * The number of remediation batches will be the length of *clusters* divided by *maxConcurrency*, each batch with a length of *maxConcurrency* containing the clusters following the *clusters* list ordering
  * The admin can make changes to *clusters* and *remediationStrategy* only in this state, it will ignore them in others.
  * The controller will transition to **UpgradeNotCompleted** state once the *remediationAction* field is set to *enforce*
* **UpgradeNotCompleted**
  * In this state, the controller will enforce the policies following the remediation plan built in the **UpgradeNotStarted** state
  * The policies of subsequent batches will start as soon as the current batch policies are all compliant unless the batch times out, in which case the controller will move on to the next batch. The value of this batch timeout is the **ClusterGroupUpgrade** timeout divided by the remediation plan number of batches.
  * Within a batch, the platform upgrade will be enforced first. Once the platform upgrade policy is compliant, then the operator upgrades policies will be enforced
  * The controller will transition to **UpgradeTimedOut** state in two cases:
    * If the **ClusterGroupUpgrade** has the first batch as canaries and the policies for this first batch are not compliant within the batch timeout
    * If the policies for the upgrade have not turned to compliant within the *timeout* value specified in the *remediationStrategy*
* **UpgradeTimedOut**
  * In this state, the controller will periodically check if all the policies for the **ClusterGroupUpgrade** are compliant, and in that case it will transition to **UpgradeCompleted**. This is to give a chance for upgrades to catch up, as they could be taking long to complete due to network, CPU or other issues but they are not really stuck and they can indeed complete
* **UpgradeCompleted**
  * In this state, the upgrades of the clusters are complete
  * If the *deleteObjects* field is set to **true**, the controller will delete the underlying RHACM policies. This is to avoid having RHACM Hub to continously check for compliance since the upgrade has been successful


## How to deploy

1. Run **make docker-build docker-push IMG=*your_repo_image***
2. Run **make deploy IMG=*your_repo_image***

Depending on how the ClusterGroupUpgrade CR is defined, the operator may create pre-caching and/or recovery workloads on the spoke clusters. To specify custom workload images, follow the examples below.
### How to deploy with pre-caching
1. Run **make docker-build docker-push IMG=*your_repo_image***
1. Run **PRECACHE_IMG=*your_precache_repo_image* make docker-build-precache docker-push-precache**
1. Run **make deploy IMG=*your_repo_image* PRECACHE_IMG=*your_precache_repo_image***

### How to deploy with failed upgrade recovery
1. Run **make docker-build docker-push IMG=*your_repo_image***
1. Run **RECOVERY_IMG=*your_recovery_repo_image* make docker-build-recovery docker-push-recovery**
1. Run **make deploy IMG=*your_repo_image* RECOVERY_IMG=*your_recovery_repo_image***

## How to test

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **cd integration_tests/*scenario***
3. Run **test.sh**

## How to develop

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **make install run**
