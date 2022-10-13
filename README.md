# Cluster Group Upgrades operator

## What is

Cluster Group Upgrades operator is a Kubernetes operator that facilitates software lifecycle management of fleets of clusters. It uses Red Hat Advanced Cluster Management (RHACM) for performing changes on target clusters, in particular by using RHACM policies.
Cluster Group Upgrades operator uses the following CRDs:

* **ClusterGroupUpgrade**

and it contains the following controllers:

* **clustergroupupgrade** that's doing both preparation steps like pre-caching and the actual cluster upgrade
* **managedclusterForCGU** used for initially deploying clusters with [Zero Touch Provisioning(ZTP)](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp)

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

The ClusterGroupUpgrade controller is designed to perform an upgrade to a group of clusters in a given period of time, which by default is set to 4h. In order to model the various phases of an upgrade, the ClusterGroupUpgrade CR can be in a variety of states.

The states generally are represented by Conditions using appropriate reasons and types. The general workflow is:

ClusterSelected -> Validated -> PrecacheSpecValid -> PrecachingSucceeded -> BackupSucceeded -> Progressing -> Succeeded

Note that if Backup is not set to true, then the BackupSuceeded condition will not be present, and similarly if precaching is not set to true then neither precaching condition will be present.

A full table of the conditions and their appropriate reasons and types are:

  | Type | Status| Reason| Message |
  |------|-------|---------|--------|
  `ClustersSelected`| True| ClusterSelectionCompleted| All selected clusters are valid| 
  | |False | ClusterNotFound | Unable to select clusters: error message |
  `Validated` | True | ValidationCompleted| Completed validation |
  | | False | NotAllManagedPoliciesExist| Missing managed policies: policyList,  invalid managed policies: policyList |
  | | False | InvalidPlatformImage | Error related to platform image |
  `PrecacheSpecValid` | True | PrecacheSpecIsWellFormed | Precaching spec is valid and consistent |
  | | False | InvalidPlatformImage| Precaching spec is incomplete |
  `PrecachingSucceeded` | True | PrecachingCompleted | Precaching is completed for all clusters|
  | | True | PartiallyDone | Precaching failed for x clusters | 
  | | False | InProgress | Precaching is not completed | 
  | | False | InProgress | Precaching is in progress for x clusters | 
  | | False | Failed | Precaching failed for all clusters |
  `BackupSucceeded` | True | BackupCompleted | Backup is completed for all clusters|
  | | True | PartiallyDone | Backup failed for x clusters |
  | | False | InProgress | Backup is in progress for x clusters|
  | | False | Failed | Backup failed for all the clusters |
  `Progressing`| True | InProgress| Remediating non-compliant policies|
  | | False | Completed | All clusters are compliant with all the managed policies |
  | | False | NotStarted | The Cluster backup is in progress |
  | | False | NotEnabled| Not enabled |
  | | False | MissingBlockingCR | Missing blocking CRs: ... |
  | | False | IncompleteBlockingCR | Blocking CRs that are not completed: ... | 
  `Succeeded`| True | Completed| All clusters compliant with the specified managed policies |
  | | False | TimedOut | Policy remediation took too long |

A few important ones to consider are:
* **ClustersSelected**
  * In this state, the list of clusters that will be considered for the **ClusterGroupUpgrade** will be checked.
  * If any of the clusters are not present then the condition will block further progress of the **ClusterGroupUpgrade**
  * The cluster list will be generated in a set order which may later be divided into batches if necessary. The order is:
    * All the clusters explicitly specified using the *cluster* option on the *ClusterGroupUpgrade* configuration (This subset will be processed in the order defined in the configuration)
    * All the clusters that match the *clusterLabelSelectors* and *clusterSelector* options on the *ClusterGroupUpgrade* configuration (This subset will be sorted in alphabetical order)
* **NotEnabled**
  * In this state, the **ClusterGroupUpgrade** CR has just been created and the *enable* field is set to *false*
  * The controller will build a remediation plan based on the *clusters* list and with *enable* fields like:
    * If *canaries* field is defined with a list of clusters, the first batch(es) of the remediation plan will contain those clusters
    * The number of remediation batches will be the length of *clusters* divided by *maxConcurrency*, each batch with a length of *maxConcurrency* containing the clusters following the *clusters* list ordering
  * The admin can make changes to *clusters*, *managedPolicies* and *enable* only in this state, it will ignore them in others.
  * The controller will transition to **InProgress** state once the *enable* field is set to *true* or to **MissingBlockingCR** or **IncompleteBlockingCR** if there are issues preventing the upgrade.
* **InProgress**
  * In this state, the controller will make copies of the inform *managedPolicies* policies. These copied policies will have their *remediationAction* set to **enforce**. Afterwards, the controller adds clusters to the corresponding placement rules following the remediation plan built in the **Progressiong+NotEnabled** state.
  * Enforcing the policies for subsequent batches starts immediately after all the clusters of the current batch are compliant with all the *managedPolicies*. If the current batch times out, then the controller moves on to the next batch. The value for the batch timeout is the **ClusterGroupUpgrade** timeout divided by the number of batches from the remediation plan.
  * The controller will transition to **TimedOut** state in two cases:
    * If the **ClusterGroupUpgrade** has the first batch as canaries and the policies for this first batch are not compliant within the batch timeout
    * If the policies for the upgrade have not turned to compliant within the *timeout* value specified in the *remediationStrategy*
* **TimedOut**
  * In this state, the controller will remove all the *managedPolicies* copies created for the **ClusterGroupUpgrade**. This is to ensure that changes are not made after the **ClusterGroupUpgrade** has passed its specified timeout. The user may re-run the **ClusterGroupUpgrade** again (perhaps with a longer timeout) if they still need to enforce changes on the clusters.
* **Completed**
  * In this state, the upgrades of the clusters are complete
  * If the *action.afterCompletion.deleteObjects* field is set to **true** (which is the default value), the controller will delete the underlying RHACM objects (policies, placement bindings, placement rules, managed cluster views) once the upgrade completes. This is to avoid having RHACM Hub to continously check for compliance since the upgrade has been successful.

## The managedclusterForCGU controller

The managedclusterForCGU controller is designed to automatically create the **ClusterGroupUpgrade** CR for each RHACM managed cluster to apply configurations generated by [Zero Touch Provisioning(ZTP)](https://github.com/openshift-kni/cnf-features-deploy/tree/master/ztp). 

It monitors the **Ready** state of each **ManagedCluster** CR on the hub cluster. For any managed cluster in the **Ready** state without a "ztp-done" label applied, the managedclusterForCGU controller automatically creates a **ClusterGroupUpgrade** CR in the "ztp-install" namespace with a list of ordered cluster associated RHACM policies that are generated during the ZTP workflow.  The clustergroupupgrade controller then remediates the set of RHACM configuration policies that are listed in the auto-created **ClusterGroupUpgrade** CR to push the configuration CRs to the managed cluster.

## Backup-recovery

Found [here](/docs/backup-recovery)
## Precaching

Found [here](/docs/pre-cache)

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
Found [here](/tests)

## How to develop

1. Export **KUBECONFIG** environment variable to point to your cluster running RHACM
2. Run **make install run**
