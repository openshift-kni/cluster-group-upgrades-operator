# Cluster Group Upgrades operator

## What is

Cluster Group Upgrades operator is a Kubernetes operator that facilitates software lifecycle management of fleets of clusters. It uses Red Hat Advanced Cluster Management (RHACM) for performing changes on target clusters, in particular by using RHACM policies.
Cluster Group Upgrades operator uses the following CRDs:

* **ClusterGroupUpgrade**

and it contains the following controllers:

* **clustergroupupgrade** that's doing both preparation steps like pre-caching and the actual cluster upgrade
* **managedclusterForCGU** used for initially deploying SNOs

## The clustergroupupgrade controller

A ClusterGroupUpgrade CR defines a desired upgrade to a group clusters.
The spec allows you to define:
* Clusters belonging to group
* Number of concurrent upgrades
* Canaries
* Desired OpenShift version
* Desired operators versions

A set of **clustergroupupgrade** example CRs can be found in the **samples** folder.

## How it works

The ClusterGroupUpgrade controller is designed to perform an upgrade to a group of clusters in a given period of time, which by default is set to 4h.
In order to model the various phases of an upgrade, the ClusterGroupUpgrade CR can be in the following states:

* **UpgradeNotStarted**
  * In this state, the **ClusterGroupUpgrade** CR has just been created and the *enable* field is set to *false*
  * The controller will build a remediation plan based on the *clusters* list and with *enable* fields like:
    * If *canaries* field is defined with a list of clusters, the first batch(es) of the remediation plan will contain those clusters
    * The number of remediation batches will be the length of *clusters* divided by *maxConcurrency*, each batch with a length of *maxConcurrency* containing the clusters following the *clusters* list ordering
  * The admin can make changes to *clusters*, *managedPolicies* and *enable* only in this state, it will ignore them in others.
  * The controller will transition to **UpgradeNotCompleted** state once the *enable* field is set to *true* or to **UpgradeCannotStart** if there are issues preventing the upgrade.
* **UpgradeCannotStart**
  * In this state, the upgrade cannot start because one the following reasons:
    * Blocking CRs are missing from the system
    * Blocking CRs have not yet reached the **UpgradeCompleted** state
* **UpgradeNotCompleted**
  * In this state, the controller will make copies of the inform *managedPolicies* policies. These copied policies will have their *remediationAction* set to **enforce**. Afterwards, the controller adds clusters to the corresponding placement rules following the remediation plan built in the **UpgradeNotStarted** state.
  * Enforcing the policies for subsequent batches starts immediately after all the clusters of the current batch are compliant with all the *managedPolicies*. If the current batch times out, then the controller moves on to the next batch. The value for the batch timeout is the **ClusterGroupUpgrade** timeout divided by the number of batches from the remediation plan.
  * The controller will transition to **UpgradeTimedOut** state in two cases:
    * If the **ClusterGroupUpgrade** has the first batch as canaries and the policies for this first batch are not compliant within the batch timeout
    * If the policies for the upgrade have not turned to compliant within the *timeout* value specified in the *remediationStrategy*
* **UpgradeTimedOut**
  * In this state, the controller will periodically check if all the policies for the **ClusterGroupUpgrade** are compliant, and in that case it will transition to **UpgradeCompleted**. This is to give a chance for upgrades to catch up, as they could be taking long to complete due to network, CPU or other issues but they are not really stuck and they can indeed complete
* **UpgradeCompleted**
  * In this state, the upgrades of the clusters are complete
  * If the *action.afterCompletion.deleteObjects* field is set to **true** (which is the default value), the controller will delete the underlying RHACM objects (policies, placement bindings, placement rules, managed cluster views) once the upgrade completes. This is to avoid having RHACM Hub to continously check for compliance since the upgrade has been successful.

## Precaching
Found [here](/docs/pre-cache)

## How to deploy

1. Run **make docker-build docker-push IMG=*your_repo_image***
2. Run **make deploy IMG=*your_repo_image***

### How to deploy with pre-caching
1. Run **make docker-build docker-push IMG=*your_repo_image***
1. Run **PRECACHE_IMG=*your_precache_repo_image* make docker-build-precache docker-push-precache**
1. Run **make deploy IMG=*your_repo_image* PRECACHE_IMG=*your_precache_repo_image***

## How to test
Found [here](/tests)

## How to develop

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **make install run**
