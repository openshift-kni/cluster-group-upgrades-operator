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

### Resolving the cluster set
Rather than handing each generated cgu the `spec.clusterLabelSelectors` and letting every cgu resolve its own cluster set, IBGU resolves the cluster set **once** and freezes it:
- At plan item 0, IBGU resolves `spec.clusterLabelSelectors` against the `ManagedClusters` and records the resulting list in plan item 0's cgu `spec.clusters`. That cgu's `spec.clusters` is the frozen record for the whole IBGU; `ManagedClusters` added after this point do not join the rollout.
- For every later plan item, IBGU reads the frozen set back from plan item 0's cgu and narrows it per plan item by evaluating the `ibgu-<action>-(completed|failed)` labels on each `ManagedCluster` (the same predicate table that used to be expressed as extra selector requirements), passing the resulting names to the cgu as `spec.clusters`.
- On every plan item the frozen set is intersected with the `ManagedClusters` that still exist. A cluster that has been deleted mid-rollout is **dropped** (so it does not block later plan items): it is reported in `status.clusters[].dropped` with a `droppedReason`, and a Warning event is emitted. `ManagedCluster` labels remain the authoritative carrier across IBGUs, so a separate `Abort` IBGU still narrows correctly.

### Terminal pre-flight blocks, plan completion, and the transient grace window

Before the first cgu is created, IBGU runs a set of **pre-flight gates**. An IBGU that fails a gate ends in a **durable terminal block** and never starts on its own later — a block that cleared hours after the operator acted would start a rollout at a time nobody chose, possibly outside the maintenance window (design `ibgu-ibu-v2-hub-design-options.md`, D2 "Empty set is terminal" and D4 "Blocks never resume on their own").

Both the terminal block and plan completion are recorded on the **`Succeeded` condition**, but they behave differently. The behaviour is keyed off the reason, not merely the presence of `Succeeded`:

| `Succeeded` | reason | kind | behaviour |
| ----------- | ------ | ---- | --------- |
| `False` | a block reason (`NoClustersMatchSelector`, `NoEligibleClusters`, `UnresolvableDenpendency`, `SeedImageUnreadable`, `BackupContentUnsupportedSeed`, `PreflightWindowExpired`) | **pre-flight block** | **permanent**: never cleared by a restart or an appended plan item. Recovery is delete-and-recreate. |
| `True`  | `Completed`             | **plan completion** | the plan ran to the end with no failure. Final for its generation; appendable (see below). |
| `False` | `CompletedWithFailures` | **plan completion** | the plan ran to the end, but at least one cluster recorded a failed action in `status.clusters[].failedActions`, or a plan-item cgu ended `Succeeded=False` (e.g. a precaching failure with no available cluster, or a batch that timed out before any cluster started). Final for its generation; appendable (see below). |

`Progressing` is mirrored to `False` with the same reason at every terminal point, so existing consumers of `Progressing` (ACM UI, ZTP, scripts) see the IBGU stopped. The gates and the plan loop otherwise keep using `Progressing` for current state.

**A pre-flight block is permanent.** Once a block is recorded, `ensureManifests` returns immediately on every later reconcile without re-running any gate or the plan loop, so neither a late-matching `ManagedCluster` nor an appended plan item can revive it.

**A plan completion is final only for the generation it was recorded against.** The completion is stamped with `metadata.generation` on the condition's `observedGeneration`. A restart, or a late-matching `ManagedCluster` (which does not change the spec), leaves the IBGU finished — its completion's `observedGeneration` still matches the current generation, so `ensureManifests` short-circuits. **Appending a plan item resumes it.** The plan is the only mutable spec field (`ibuSpec` and `clusterLabelSelectors` are immutable; the plan is append-only), so a completion whose `observedGeneration` is *older* than the current generation means items were appended: IBGU drops `Succeeded`, sets `Progressing=InProgress`, and runs the plan loop. Earlier plan items keep their cgus (the pre-flight gates do not run again), and appended items act on the **frozen cluster set** recorded in plan item 0's cgu — never a re-resolved selector. `Succeeded` is re-written with the new generation when the appended items finish. This is what keeps the documented incremental workflow (["Step by step"](#step-by-step)) working, including appending `AbortOnFailure` after a `CompletedWithFailures` run to abort the clusters that failed.

**Transient vs terminal (pre-flight gates).** Each gate's failure is classified by whether its verdict is definitive on content actually read, or could be GitOps-sync / propagation lag (the manager's cached client can return `NotFound` for a `BackupContent`/`ConfigMap` or a `ManagedCluster` label that a single sync applied alongside the IBGU):

- **Transient** (retried within a grace window, then a terminal block with the last reason): no `ManagedCluster` matches the selector (`NoClustersMatchSelector`); the base set is non-empty but none is eligible for plan item 0's action (`NoEligibleClusters`); an unresolvable `backupContent`/`oadpContent` reference (`UnresolvableDenpendency`); and a seed image that cannot be read — registry unreachable, pull secret missing/unreadable, transport error (`SeedImageUnreadable`).
- **Immediately terminal** (a definitive verdict on content actually read): `spec.ibuSpec.backupContent` paired with a seed image older than the first format that supports it (`BackupContentUnsupportedSeed`).

If every gate passes but the last eligible `ManagedCluster` is deleted in the race before plan item 0's cgu can be created, the empty selection at first-cgu creation is treated as the empty-set block (transient, then terminal), never as a plan completion — a one-item plan can never report `Succeeded=True` with no cgu.

The gates run in order **empty cluster set → references → seed format**, so a hub-only, deterministic failure (a selector typo, a missing `BackupContent`) is reported with its own reason before any registry read.

**Grace window.** A transient failure is retried until a bounded grace window elapses, then becomes a terminal block with the last reason. The window is measured from the IBGU's `metadata.creationTimestamp` — the moment the operator acted — so it cannot be reset or lost, and an IBGU first reconciled more than the window after creation (for example after a TALM outage) goes straight to a terminal block. The window also bounds the **passing** path: even when every gate passes, the first cgu must be created within the window. If TALM was down past the window and recovered, a now-passing pre-flight is blocked terminally (`PreflightWindowExpired`) rather than starting a rollout hours late. This applies only before the first cgu — appended items (cgus already exist) are never gated on the window. The default window is **300 seconds (5 minutes)**, overridable per IBGU with the annotation `lcm.openshift.io/ibgu-preflight-retry-seconds` (a non-negative integer number of seconds; `0` makes the first transient failure terminal). An invalid value falls back to the default and raises a Warning event. Editing the annotation has **no effect once the IBGU is already terminally blocked**.

> **Release note.** An empty IBGU (no `ManagedCluster` matches the selector, or none is eligible for plan item 0) no longer reports success; after a bounded grace window it is blocked terminally (`Succeeded=False`) and creates no cgu. An unresolvable reference or an incompatible seed no longer auto-heals; recovery is delete-and-recreate. A plan that finishes with a failed cluster action (or a plan-item cgu that ends `Succeeded=False`) now reports `Succeeded=False`, reason `CompletedWithFailures`. A pre-flight that only passes after its grace window has expired is blocked terminally (`PreflightWindowExpired`) instead of starting a late rollout. Appending a plan item to a *completed* IBGU still resumes it; only a terminal pre-flight block requires delete-and-recreate.

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

# BackupContent

`BackupContent` (`lcm.openshift.io/v1alpha1`) is a hub-side, namespaced kind that holds a
curated backup definition for an upgrade path. One object is one backup definition, and it
outlives any single IBGU so definitions can be reused across upgrades. Its `spec` is LCA's
exported `BackupContentSpec` (`lca.openshift.io/v1`) type directly, with no wrapper field, so
a hub entry and its spoke copy differ only in `apiVersion` and namespace. During IBGU `Prep`,
TALM resolves each `ibuSpec.backupContent` reference to the hub `BackupContent` of that name in
the IBGU's namespace and copies it into the Prep `ManifestWork` as a spoke `BackupContent`
(`lca.openshift.io/v1`) in the `openshift-lifecycle-agent` namespace, with the hub spec verbatim
and all hub metadata (resourceVersion, managedFields, creationTimestamp, uid, ownerReferences,
generation, labels) stripped. The Prep `ManifestWork` uses the `Orphan` delete option, so the
copies survive on the spoke after the Prep `ManifestWork` is deleted, which is when LCA re-reads
them at pre-pivot. TALM does not create the `openshift-lifecycle-agent` namespace; a spoke whose
LCA is installed elsewhere fails loudly at apply time.

`spec` is immutable (enforced by a CEL `self == oldSelf` validation): updating an existing
`BackupContent` is rejected by admission, so a content change is a new entry and a new IBGU.
Deleting and recreating is allowed. The object name is reused verbatim as the spoke
`BackupContent` name, so it must be a valid LCA (RFC-1123) object name.

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: BackupContent
metadata:
  name: ibu-platform-and-apps
  namespace: default
spec:
  backupWave: 2
  restoreWave: 2
  namespaces:
    - open-cluster-management-agent
  namespacedResources:
    - kind: deployments.apps
      names:
        - klusterlet
  clusterScopedResources:
    - kind: clusterroles.rbac.authorization.k8s.io
      names:
        - klusterlet
```
