# Use case examples
## Requirements
Spoke cluster must have LCA and OADP operators installed.
Platform backup configmap can be created on the hub cluster and IBGU will propagate it to spokes.
```
$ cat oadp.yaml
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: acm-klusterlet
  annotations:
    lca.openshift.io/apply-label: "apps/v1/deployments/open-cluster-management-agent/klusterlet,v1/secrets/open-cluster-management-agent/bootstrap-hub-kubeconfig,rbac.authorization.k8s.io/v1/clusterroles/klusterlet,v1/serviceaccounts/open-cluster-management-agent/klusterlet,scheduling.k8s.io/v1/priorityclasses/klusterlet-critical,rbac.authorization.k8s.io/v1/clusterroles/open-cluster-management:klusterlet-work:ibu-role,rbac.authorization.k8s.io/v1/clusterroles/open-cluster-management:klusterlet-admin-aggregate-clusterrole,rbac.authorization.k8s.io/v1/clusterrolebindings/klusterlet,operator.open-cluster-management.io/v1/klusterlets/klusterlet,apiextensions.k8s.io/v1/customresourcedefinitions/klusterlets.operator.open-cluster-management.io,v1/secrets/open-cluster-management-agent/open-cluster-management-image-pull-credentials" 
  labels:
    velero.io/storage-location: default
  namespace: openshift-adp
spec:
  includedNamespaces:
  - open-cluster-management-agent
  includedClusterScopedResources:
  - klusterlets.operator.open-cluster-management.io
  - clusterroles.rbac.authorization.k8s.io
  - clusterrolebindings.rbac.authorization.k8s.io
  - priorityclasses.scheduling.k8s.io
  includedNamespaceScopedResources:
  - deployments
  - serviceaccounts
  - secrets
  excludedNamespaceScopedResources: []
---
apiVersion: velero.io/v1
kind: Restore
metadata:
  name: acm-klusterlet
  namespace: openshift-adp
  labels:
    velero.io/storage-location: default
  annotations:
    lca.openshift.io/apply-wave: "1"
spec:
  backupName:
    acm-klusterlet

$ oc create configmap oadp-cm -n openshift-adp --from-file oadp.yaml
```
Notice that `.metadata.annotations["lca.openshift.io/apply-label"]` has one more item compared to normal IBU platfrom backup.
`rbac.authorization.k8s.io/v1/clusterroles/open-cluster-management:klusterlet-work:ibu-role` this object is used by manifest work agent to access IBU object and it should be restored after pivot or else CGU can not see the status of IBU.

## Step by step
User can start the upgrade for a set of clusters by creating an IBGU with `Prep` action.
```yaml
    plan:
    - actions:        
      - Prep   
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 15
```
After the actions is completed, user can abort the failed cluster if there are any, by adding `AbortOnFailure` to the plan
```yaml
    plan:                     
    - actions:        
      - Prep   
      rolloutStrategy:            
        maxConcurrency: 200
        timeout: 15
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
```

The following command can be used to append an item to plan list:
```
oc patch ibgu <ibgu-name> --type=json -p '[{"op": "add", "path": "/spec/plan/-", "value": {"actions": ["AbortOnFailure"], "rolloutStrategy": {"maxConcurrency": 200, "timeout": 30}}}]'
```

After completion, user can continue the upgrade by adding `Upgrade` to the plan. This action only selects the managed clusters that have successfully completed prep.
``` yaml
    plan:
    - actions:
      - Prep
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 15
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - Upgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 60
```
Similar to the prep, after action completion user can abort cluster that have failed upgrade by adding `AbortOnFailure` to the plan.
``` yaml
    plan:
    - actions:
      - Prep
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 15
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - Upgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 60
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
```
Finally, user can finalize the upgrade by `FinalizeUpgrade` to the plan. 
``` yaml
    plan:
    - actions:
      - Prep
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 15
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - Upgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 60
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - FinalizeUpgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
```
## All actions together
User can create all the actions from the beginning without the need to manually add actions after previous action is completed. 
``` yaml
    plan:
    - actions:
      - Prep
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 15
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - Upgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 60
    - actions:
      - AbortOnFailure
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
    - actions:
      - FinalizeUpgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 10
```
By doing this after each action item is completed the next action item automatically starts. Note that using this method, clusters that fail `Prep` or `Upgrade` automatically are aborted and transition to `Idle` state and there will be no time for troubleshooting them.
## Single CGU with manual abort
User can put all actions in one item, IBGU will create only one cgu for all of the actions.
```yaml
    plan:
    - actions:
      - Prep
      - Upgrade
      - FinalizeUpgrade
      rolloutStrategy:
        maxConcurrency: 200
        timeout: 200
```
In case there are clusters that have failed `Prep`or `Upgrade`, user can create a separate IBGU with `Abort` action and select the clusters manually using `spec.clusterLabelSelectors`.

Similarly to rollback successful upgrades or to abort successful prep, a separate IBGU can be created.

# Reconcile
## Ensuring manifests
IBGU lists all cgus that have this ibgu as their owner via ibgu label.
IBGU goes through all plan items in the ibgu and generates the corresponding cgu for that plan item. 
- If the cgu exist
    - if cgu is completed, ibgu moves on to the next plan item.
    - if cgu is not completed, reconcile again until it is completed.
- if the cgu does not exist, create the cgu
### Creating cgu for plan item
for every action in the plan item, generate the corresponding IBU. Create a manifestworkreplicaset that contains the ibu and the clusterrole that allows manifestwork agent to modify ibu.
For prep, the manifestworkreplicaset also conatins the manifests in the `oadpContent`. Also if the seed image pull secret is present on the hub, in the same namespace as the IBGU CR, controller will add the secret to manifest list. The secret will be recreated on the spokes in the `openshift-lifectcycle-agent` namespace.
After creating all the manifestworkreplicasets create a cgu containing the manifestworkreplicasets. If the upgrade action is in the plan item, the cgu will disable auto import before start and re enables it after completion.

## Sync status
ibgu lists all cgus that are created by this ibgu and sorts them by the planIndex so that order of actions in the status will be correct. It then reads the status of cgus and add failed and completed actions to clusters field of ibgu. For failed actions ibgu also gathers the reason and message for that condition.

Example of ibgu status:

```yaml
status:
  clusters:
  - completedActions:
    - action: Prep
    - action: AbortOnFailure
    failedActions:
    - action: Upgrade
      message: "failed upgrade"
    name: spoke1
  - completedActions:
    - action: Prep
    - action: Upgrade
    - action: FinalizeUpgrade
    name: spoke4
  - completedActions:
    - action: AbortOnFailure
    failedActions:
    - action: Prep
      message: "failed prep"
    name: spoke6

```

## Labelling managed clusters
Some ibgu actions only run on managed clusters that have certain labels

| action             | managed cluster label                                                         |
| ------------------ | ----------------------------------------------------------------------------- |
| `FinalizeUpgrade`  | `lcm.openshift.io/ibgu-upgrade-completed`                                     |
| `FinalizeRollback` | `lcm.openshift.io/ibgu-rollback-completed`                                    |
| `AbortOnFailure`   | `lcm.openshift.io/ibgu-upgrade-failed` or `lcm.openshift.io/ibgu-prep-failed` |
| `Upgrade`          | `lcm.openshift.io/ibgu-prep-completed`                                        |
| `Rollback`         | `lcm.openshift.io/ibgu-upgrade-completed`                                     |

Notice that Rollback only selects clusters that have upgrade completed label. If upgrade fails almost always the hub cluster and IBGU controller can not manage the spoke cluster. So we rely on the IBU autorollback to handle failed upgrades.

`Abort` ibgu action only runs when `lcm.openshit.io/ibgu-upgrade-completed` is not present.

IBGU applies these labels according to its current status. After a successful `Abort`, `FinalizeUpgrade`, `FinalizeRollback` or `AbortOnFailure` IBGU removes these labels from the respective managed cluster.
