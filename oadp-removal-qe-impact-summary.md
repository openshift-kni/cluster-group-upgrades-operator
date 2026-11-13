# Remove OADP from IBU — Design Summary for QE

Audience: QE, to scope functional, system, automation and performance/scale coverage for
epic **CNF-26657** "Reduce IBU downtime during upgrade - Remove OADP requirement"
(fixVersion openshift-5.1).

This summarises decisions recorded elsewhere; it does not make new ones. Sources:
the hub-side design record (`ibgu-ibu-v2-hub-design-options.md`, branch `ibgu-options`,
section "Decision record"), and the Jira tickets listed at the end. Where a detail is
still open, it is called out in [Open items that affect the test plan](#open-items-that-affect-the-test-plan).

---

## 1. What the feature is, in one page

**Goal (epic):** reduce downtime during an image-based upgrade (IBU) by removing the OADP
operator from backup and restore. Reducing *rollback* time is explicitly not a goal.

### What ships, as one unit (CNF-28772)

These four arrive in the same LCA release and are backported together to every z-stream
that receives any of them. The unit is never split.

| Piece | What it does | Ticket |
|---|---|---|
| OADP removal | LCA's own local backup/restore replaces OADP/Velero. Still reads the Velero-format content in `oadpContent` ConfigMaps. | CNF-22167 (PR#7818) |
| Seed format bump | `SeedFormatVersion` goes from 4 to 5. Every existing seed image is invalidated and must be rebuilt with the new `lca-cli`. | CNF-22167 |
| BackupContent API | New LCA CRD `BackupContent` (`lca.openshift.io/v1`, plural `backupcontents`, namespaced; spec type `BackupContentSpec`) and a new optional IBU v1 field `backupContent` listing those objects by name. References resolve in LCA's install namespace (default `openshift-lifecycle-agent`, exported as `ibuv1.DefaultNamespace`). At most one of `backupContent` and `spec.oadpContent` may be set; neither is valid. | CNF-26659 (API, PR #10403), CNF-29691 (engine), CNF-28671 |
| Seed format publication | LCA publishes the seed format it expects as a ClusterClaim and as a field on the IBU singleton's status (`status.seedFormatVersion`, working name), and exports the related constants from its API package. | CNF-28926 |

### Hub side (TALM / IBGU)

| Change | Story |
|---|---|
| Revendor LCA; IBGU gains `ibuSpec.backupContent` through the embedded spec, and inherits the at-most-one rule. No new IBGU version or kind. | CNF-29239 |
| New hub CRD `BackupContent` (`lcm.openshift.io/v1alpha1`, namespaced): one curated backup definition per object, embedding LCA's BackupContent spec type. Same kind name as LCA's spoke CRD, different API group. Spec is immutable. | CNF-29240 |
| TALM copies the referenced hub BackupContent entries to the spoke as LCA BackupContent objects, in the Prep manifestwork, into `ibuv1.DefaultNamespace` (`openshift-lifecycle-agent`, LCA's default install namespace), same name. Spokes with LCA installed elsewhere are not supported and fail loudly. | CNF-29241 |
| Pre-flight, image side, before the first CGU: read the seed image's format label and reject `backupContent` with a pre-bump seed (hub-only safety rule). Today runs only for IBGUs that set `backupContent`. | CNF-29242 (PR #14404) |
| Pre-flight, spoke side: per-cluster seed format from the LCA claim (no claim on an available cluster = pre-bump LCA; requires hub ACM 2.14+); compared with the image's format for every IBGU. | CNF-30039 |
| Pre-flight blocks never resume on their own; outcome on the `Succeeded` condition; appending to a completed IBGU still works. | CNF-30061 (merged) |
| Simplification: every pre-flight failure is terminal at once (no retries, no annotation), a fixed 5-minute start deadline, uncached reads in the pre-flight. | CNF-30176 |
| Unresolvable references (hub BackupContent or `oadpContent` ConfigMap) block the IBGU instead of being silently skipped. | CNF-29243 |
| IBGU resolves the cluster list once (independent of OADP removal). | CNF-29244 |
| Condition types from LCA's API; LCA stage failures surfaced in IBGU status promptly (report only: the CGU state machine is unchanged, so a failed cluster still waits out its batch's timeout share). | CNF-29245 |
| Upstream docs, samples, kuttl. | CNF-29247 |
| Bounded per-cluster status on CGU timeout (pre-existing scale issue, found during this design). | CNF-29281 |

### What does *not* change (a regression surface, not a new one)

- IBU API version stays `v1`. Existing IBUs, `spec.oadpContent`, the ConfigMap and Velero CR
  format, ZTP policies and existing IBGUs keep working.
- IBGU API version stays `v1alpha1`.
- Image-based install (IBI) is unaffected: the seed format check runs only in IBU Prep.

### Customer migration: two optional steps, one release

Step 1 is required to adopt the release; step 2 is optional until a future release that
removes `oadpContent` (TBD).

1. **Adopt the release** (OADP removal + seed bump): rebuild seeds; roll out in order —
   seed cluster's LCA, then rebuild and publish seeds, then target clusters' LCA. Keep
   publishing both seed images during the fleet transition. OADP, S3 and the DPA CR can
   be removed.
2. **Move to BackupContent**: convert backup content offline to BackupContent CRs (reference CRs
   from CNF-28676); create a *new* IBGU (`ibuSpec` is immutable).

Sample manifests for every object involved — before and after — are in
[Appendix A](#appendix-a--sample-manifests).

---

## 2. Behaviour QE must be able to predict

### 2.1 The core compatibility matrix

"Pre-bump" = an LCA or seed from before this release. The seed check in LCA Prep is a hard
equality between the seed image's format label and the LCA's compiled-in constant
(`lifecycle-agent/controllers/prep_handlers.go:229-246`).

| Spoke LCA | Seed | IBU backup input | Direct IBU (oc apply) | Direct IBU (GitOps / ACM policy) | IBGU |
|---|---|---|---|---|---|
| new | new | `backupContent` | works | works | works |
| new | new | `oadpContent` | works (deprecated path) | works | works |
| new | new | neither | works, no app backup; Prep message says so | same | same |
| new | pre-bump | any | Prep fails: seed format mismatch | same | **blocked by pre-flight** before any CGU |
| pre-bump | new | any | Prep fails: seed format mismatch | same | **blocked by pre-flight** |
| pre-bump | pre-bump | `oadpContent` | works as today (OADP still required on that LCA) | same | works as today |
| pre-bump | pre-bump | **`backupContent`** | **rejected** by strict field validation (oc/kubectl default): unknown field | **silently pruned; upgrades with no application backup** | **rejected on the hub** by the safety rule |
| any | any | both | rejected at admission (CEL) | rejected | rejected at IBGU admission |

The highlighted row is the one silent-data-loss case in the feature. It is closed for IBGU
by TALM's safety rule, and for direct IBUs only by documentation ("use `backupContent` only
with a new-format seed").

### 2.2 IBGU pre-flight decision order (CNF-29242, CNF-30039, CNF-30061, CNF-30176)

Runs once, before the first CGU. Every outcome must surface as an IBGU condition naming the
cluster and the reason.

1. **Hub-only safety rule:** IBGU sets `backupContent` and the seed image's format (read from
   its OCI label on the registry) is older than the first format that supports
   `backupContent` → IBGU rejected. No spoke is read.
2. Per cluster:
   1. `ManagedClusterConditionAvailable` not True → blocked, "cluster unavailable".
   2. LCA ClusterClaim present → use its seed format.
   3. Claim absent → the spoke's LCA predates the bump (pre-bump format). Definitive on
      hub ACM 2.14+, where the claim's reserved suffix means it is never truncated; there
      is no fallback read.
   4. Spoke's expected format ≠ image's → blocked, "seed format mismatch".
3. Image label unreadable (registry unreachable, auth failure, label missing) → blocked.
   CNF-29242 reads the label only for IBGUs that set `backupContent`; from CNF-30039 it is
   read for every IBGU, for the per-spoke comparison.
4. Opt-out for lab use: `lcm.openshift.io/ibgu-skip-seed-compatibility-check` (PR #14404)
   skips the seed-format check. It is recorded as a `SeedCompatibilityCheckSkipped` event,
   not as a condition.
5. Plan item 0 resolves to no clusters (no ManagedCluster matches the selector, or none is
   eligible for plan item 0's action) → blocked, with a reason naming which (CNF-30061).

**How blocks behave (CNF-30061, simplified by CNF-30176).** An unattended, delayed start is
service-impacting, so no block ever clears on its own:
- **Every pre-flight failure is terminal at once:** empty cluster set, unresolvable
  reference, unreadable seed image (registry unreachable, pull-secret read error, HTTP-only
  registry, oversized config), `backupContent` with a pre-bump seed, and with CNF-30039 an
  unavailable spoke or a seed-format mismatch. No retries and no requeue; the IBGU stays
  blocked after the cause is fixed, across a TALM restart and a plan append, and retrying
  means deleting and recreating it.
- **Start deadline:** if the first CGU would be created more than 5 minutes after the
  IBGU's creation (TALM was down and came back outside the maintenance window), the IBGU
  blocks with `PreflightWindowExpired` even though every gate passes. Fixed, no
  annotation. It bounds only the first start, not appended plan items.
- **Uncached reads:** the pre-flight reads BackupContent, ConfigMaps and ManagedClusters
  through the API reader, so cache lag cannot cause a false block. GitOps users should
  apply BackupContent before the IBGU (sync waves).
- *CNF-30061 as merged still retries some failures within a 5-minute window configurable
  with `lcm.openshift.io/ibgu-preflight-retry-seconds`; CNF-30176 removes that. Test
  against the build in use.*
- **Outcome condition:** a finished IBGU carries `Succeeded` — True/`Completed` when every
  plan-item CGU succeeded and every cluster made it, False/`CompletedWithFailures` when the
  plan finished but a CGU ended False or a cluster failed, False with the block reason when
  a pre-flight block turned terminal. Progressing goes False with the same reason.
- **Appending still works:** appending a plan item to a *completed* IBGU (for example
  `['Upgrade']` after `[['Prep']]`) resumes it on the same frozen cluster set, with no
  pre-flight and no new clusters; `Succeeded` is removed while it runs and written again
  at the end. Appending to a *blocked* IBGU does nothing; it stays blocked.
- **Order:** empty set, then references, then the seed.
- **Existing IBGUs** finished before the TALM upgrade, including ones that completed with
  no CGU, get a `Succeeded` condition on the first reconcile and stay finished; they
  don't start.

### 2.3 Lifetime of copied BackupContent objects

- LCA reads its BackupContent objects **twice**: validated in Prep, re-read at Upgrade
  pre-pivot where the backup is taken. One missing or invalid at pre-pivot fails the Upgrade
  terminally, before the pivot (CNF-29691).
- TALM ships copies only in the Prep manifestwork, with `DeleteOption: Orphan`, so they
  survive the CGU deleting that manifestwork before Upgrade runs.
- Content is **pinned** when the Prep CGU starts: editing a hub BackupContent afterwards is
  not propagated (and is blocked anyway, since its spec is immutable).
- A follow-up IBGU whose plan is Upgrade-only copies nothing and must carry the same
  `backupContent` list; LCA refuses a changed list on an in-progress IBU.
- Copies are never reaped by TALM; the next path's Prep overwrites them by name.

---

## 3. Functional test coverage

### 3.1 LCA (spoke)

| Area | What to cover | Source |
|---|---|---|
| CEL on IBU | both inputs set → rejected; neither → accepted (incl. the Idle IBU created by InitIBU); changing `backupContent` while in progress → rejected | CNF-28671 |
| Spoke BackupContent schema | required `kind` per entry; types, bounds; unknown fields pruned silently (accepted); strict-validation guidance | CNF-26659, CNF-28671 |
| Reference validation (Prep) | dangling or duplicate reference fails Prep naming the entry; all failures collected in one pass | CNF-28671 |
| Semantic validation (Prep) | unresolvable kind, singular forms (`job` vs `jobs`), wrong scope, ambiguous bare kind, missing namespace, wave coherence, `names` that don't exist, `labelSelector` semantics (filters only entries without `names`) | CNF-28671 |
| Pre-pivot re-validation | Spoke BackupContent edited invalid or deleted after Prep → terminal Upgrade failure before pivot, not requeued | CNF-29691, CNF-28671 |
| Install namespace | references resolve in LCA's install namespace (`MY_POD_NAMESPACE`), same as `seedImageRef.pullSecretRef`; with LCA installed outside `openshift-lifecycle-agent`, a BackupContent copied into `openshift-lifecycle-agent` is a dangling reference and fails Prep | CNF-28671 |
| Backup content | namespaced and cluster-scoped resources, `names`, exclusions, `labelSelector`, `backupWave`/`restoreWave` ordering (independent; omitted = last wave) | CNF-26659 |
| Restore | PV `claimRef` always cleared; `logicalvolumes` status always restored; server-side dry-run of everything before first apply, failures collected, abort without mutation; CRD-not-yet-established case | CNF-26659, CNF-28671 |
| Backup-source message | `PrepCompleted` message names the validated BackupContent objects, or `oadpContent` ConfigMaps with deprecation note, or "no application backup configured"; composes with the extramanifests warning | CNF-28671 |
| Deprecated path | `oadpContent` behaviour unchanged, including existing `ValidateBackupConfigmaps` checks | CNF-28671 |
| On-disk layout | stable across the pivot: `<wave>_<name>/`, `<NNNNN>_<kind>_<namespace>_<name>.yaml`, `checksums.sha256`, identical from both input paths; versioned by `backup-layout.json` (semver, 1.0.0) independently of the seed format; reader stays backward compatible with older layout versions | CNF-26659, CNF-29691 |
| Seed format check | mismatch either direction fails Prep; missing label fails Prep | CNF-28772 |
| Publication | ClusterClaim appears in `ManagedCluster.status.clusterClaims` on the hub with the seed format; the IBU's `status.seedFormatVersion` carries the same value, rewritten on every LCA start (so the post-pivot LCA overwrites the value `InitIBU` restores); both correct across an LCA operator upgrade and across the pivot | CNF-28926 |
| Condition types | names unchanged; exported from the API package | CNF-28751 |

### 3.2 TALM / IBGU (hub)

| Area | What to cover | Story |
|---|---|---|
| IBGU admission | both inputs → rejected (inherited at-most-one CEL, verified in the regenerated CRD); duplicate `backupContent` names → rejected; `backupContent` only, `oadpContent` only, neither → accepted; `ibuSpec` still immutable | CNF-29239 |
| Hub BackupContent CRD | create/delete; spec update rejected; namespaced; bundled in the CSV | CNF-29240 |
| Copy to spoke | Spoke BackupContent created in `openshift-lifecycle-agent` (`ibuv1.DefaultNamespace`) with the entry's name and spec; pull secret still copied there; on a spoke without that namespace the Prep manifestwork reports the apply failure; metadata stripped (no `ownerReferences`, `uid`, hub labels); survives deletion of the Prep manifestwork; present at Upgrade pre-pivot | CNF-29241 |
| Work-agent RBAC | first Prep after a TALM upgrade applies BackupContent objects without a lingering `Applied=False` (ClusterRole applied first; `backupcontents` rule for `lca.openshift.io` present) | CNF-29241 |
| Reference resolution | Prep plan with a missing hub BackupContent → blocked with names, no CGU; same for a missing `oadpContent` ConfigMap (**behaviour change** from today's warn-and-skip); Upgrade-only plan not blocked by missing hub entries | CNF-29243 |
| Pre-flight | each branch in §2.2, including: claim present and matching; claim present and mismatching; claim absent (pre-bump); cluster unavailable; image label unreadable; safety rule; opt-out annotation (skip recorded as an event) | CNF-29242, CNF-30039 |
| Block behaviour | each terminal reason stays blocked after the cause is fixed, across a TALM restart and a plan append; recreating proceeds; every pre-flight failure (including an unreachable registry) blocks on the first reconcile with no requeue; a BackupContent that exists when the IBGU is first reconciled is found even if TALM's cache lags; an IBGU first reconciled more than 5 minutes after creation blocks with `PreflightWindowExpired` even when every gate passes; the retry annotation has no effect after CNF-30176; Progressing goes False with the reason; `Succeeded` is True/`Completed`, False/`CompletedWithFailures` or False/block reason as appropriate; an IBGU that finished before the TALM upgrade gets `Succeeded` and does not start afterwards; appending to a completed IBGU (`[['Prep']]` then `['Upgrade']`, `CompletedWithFailures` then `['AbortOnFailure']`) resumes it on the frozen set and re-writes `Succeeded`, including for an IBGU migrated by the upgrade; appending to a blocked IBGU does not revive it; a plan-item CGU that ended `Succeeded=False` with no per-cluster failure still yields `CompletedWithFailures` | CNF-30061, CNF-30176 |
| Status | per-cluster failure messages in IBGU status for LCA stage failures, without waiting for timeout | CNF-29245 |
| Plans | Prep → Upgrade → FinalizeUpgrade; Prep+Upgrade in one item; Rollback → FinalizeRollback; Abort; AbortOnFailure; follow-up Upgrade-only IBGU; separate Abort IBGU | existing + CNF-29241 |
| Cluster list | frozen set; ManagedCluster deleted mid-rollout is dropped and reported, not blocking; a ManagedCluster labelled after the IBGU started is not picked up; plan item 0 with no eligible clusters → terminal block | CNF-29244, CNF-30061 |

### 3.3 Negative and failure injection

- `backupContent` + pre-bump seed via IBGU → rejected on the hub (the safety rule).
- Same combination via ACM policy / GitOps against a pre-bump spoke → **expect silent
  no-backup**; this test documents the accepted limitation and the doc guidance.
- BackupContent deleted on the spoke between Prep and Upgrade → terminal Upgrade failure.
- BackupContent deleted on the hub after Prep → no effect on the running IBGU.
- Registry unreachable / wrong pull secret / seed image without the label → IBGU blocked
  terminally on the first reconcile; recreate once fixed (CNF-30176).
- Selector that matches no ManagedCluster (typo) → terminal block, not a green IBGU
  (CNF-30061).
- Two IBGUs acting on the same cluster at once → unsupported. It is not yet rejected
  (CNF-29901, a future improvement), so don't test it as a supported flow; the
  `ibgu-<action>-*` labels and same-named spoke BackupContent objects collide.
- LCA namespace deleted on the spoke after Prep → copies lost → pre-pivot failure (doc
  caveat: content dies with the namespace).
- More than 20 custom ClusterClaims on a spoke → LCA's claim is still present (reserved
  `openshift.io` suffix, exempt from the cap on ACM 2.14+) and the check proceeds.
- Spoke unreachable mid-pre-flight → blocked "unavailable" (stale claim not trusted).

---

## 4. System and upgrade testing

| Scenario | Why it matters |
|---|---|
| Full migration step 1 on a fleet: seed cluster LCA → rebuild seeds → target LCA, with both seed images published | ordering is non-obvious; out of order, upgraded targets have no usable seed |
| Mixed fleet during the transition window: one IBGU per seed format, pre-flight blocks the wrong pairing | the realistic state of a customer fleet |
| LCA z-stream that received the backport unit vs one that did not | the unit must not be split; verify publication and BackupContent support appear together |
| LCA operator upgrade with an automatic InstallPlan | can invalidate a pre-staged seed; guidance recommends manual approval |
| LCA operator rollback to the previous z-stream | new-format seeds become unusable; seed must be reverted too |
| LCA upgrade while an IBU is not Idle | the Idle precondition becomes a hard requirement in the guidance (CNF-28772); verify the behaviour if it is violated |
| Step 2 migration: existing `oadpContent` IBGU kept, new IBGU using `backupContent` created | coexistence on one hub; no policy goes NonCompliant |
| TALM upgrade (revendor) with IBGUs in flight | existing IBGUs and CGUs unaffected; new checks apply only to new IBGUs; blocked IBGUs stay blocked and completed ones stay completed across the restart |
| ZTP / RDS reference CRs (CNF-28676) applied end to end, including `PlatformBackupRestoreWithIBGU` | reference config is what customers copy |
| Disconnected hub with a mirror registry | the hub must now reach the registry and trust its CA to read the seed label |
| Hub behind a cluster-wide proxy | registry egress path for the label read |
| Rollback after a failed restore (auto-rollback) | restore failures must stay terminal and roll back cleanly |
| Spoke with OADP still installed after step 1 | the guide says OADP can be removed; confirm leaving it installed does not interfere |

---

## 5. Automation

| Layer | Owner | Notes |
|---|---|---|
| LCA unit and envtest | LCA dev | CEL rules (envtest), validation branches, dry-run abort, backup-source message — listed in CNF-28671's acceptance criteria |
| TALM unit | TALM dev | manifest generation and ordering, metadata stripping, pre-flight branches, reference resolution, status mapping, the 3,500-cluster status-size test (CNF-29281) |
| TALM kuttl (upstream CI) | TALM dev | Prep+Upgrade with `backupContent`; missing reference blocked; pre-flight block (CNF-29247). kuttl here has no real spoke, so claims, the IBU status field and registry labels must be stubbed |
| QE E2E | QE | real hub + spokes for: the compatibility matrix rows in §2.1, migration ordering, LCA rollback, disconnected registry, claim truncation, downtime measurement. Test plans in Polarion and automation merged are epic exit criteria |

Hard-to-simulate inputs QE will need tooling for: a pre-bump LCA spoke alongside new ones;
seed images of both formats; a spoke with more than 20 custom ClusterClaims; a registry
that can be made unreachable from the hub only.

---

## 6. Performance and scale

### 6.1 The headline measurement: IBU downtime

The epic's acceptance criteria require documented improvement times and the
configurations tested. Measure, on the same hardware and workload:

- downtime with OADP (baseline, pre-bump LCA) vs without (this release), for both input
  paths (`oadpContent` and `backupContent`);
- broken down by stage: pre-pivot backup, pivot/reboot, post-pivot restore;
- as backup content grows (number of resources, PVs, namespaces);
- the cost of the new pre-restore dry-run, which adds time on the restore path.

Rollback time is out of scope for the epic; record it, but it is not a pass/fail target.

### 6.2 Hub-side scale (use the fleet size QE normally targets; the design analysis used 3,500 spokes)

| Item | Expected behaviour | What to measure |
|---|---|---|
| Pre-flight, claims present | one List of ManagedClusters for the fleet | time to first CGU |
| Seed label read | once per IBGU, not per cluster | single registry call |
| BackupContent copies | small objects in the Prep manifestwork, one per referenced entry per spoke | manifestwork size with realistic entry counts |
| CGU object size | normal rollout: well under the limit even with 2B's explicit list (~383 KiB at 3,500 clusters with 25-character names) | status size at end of rollout |
| **CGU object size on mass timeout** | today about 770–780 timed-out clusters in one CGU fill the ~1.5 MiB object (each carries ~1.2–2 KB of ManifestWork status); the CGU then cannot write status | reproduce with a fleet-wide timeout above that size; verify the fix in CNF-29281 |
| Fleet-wide LCA stage failure (report only) | failure messages appear in IBGU status within a reconcile, but every batch still runs to its timeout share | time until the failure is visible; total wall-clock, as a baseline |

---

## 7. Known limitations and accepted risks (test as documented behaviour)

- Unknown fields in a spoke BackupContent are pruned silently by the API server; strict field
  validation is the guidance.
- `backupContent` with a pre-bump seed on a pre-bump LCA, delivered by GitOps or policy,
  upgrades with no application backup. Only IBGU blocks it.
- No status on the spoke BackupContent: outcomes are visible only in IBU conditions and LCA logs.
- No Velero hooks: no quiesce/unquiesce for databases.
- `restorePVs` and `restoreStatus` removed: no manual snapshot of dangling volumes after
  the pivot; status restored only for `logicalvolumes`.
- Copied BackupContent objects on the spoke are not reaped; deleting the LCA namespace removes
  pending backup input.
- Hub BackupContent edits after Prep has started are not propagated (spec is immutable).
- TALM's hard-fail on a missing `oadpContent` ConfigMap is a behaviour change for existing
  users (previously warn and continue with no backup).
- `BackupContent` is the kind name on both hub (`lcm.openshift.io`) and spoke
  (`lca.openshift.io`). TALM and LCA never share a cluster (no SNO is ever a hub), so a
  bare `oc get backupcontent` is unambiguous in practice; test tooling may still use
  `backupcontents.lcm.openshift.io` or `backupcontents.lca.openshift.io` for clarity.
- The hub supports only spokes with LCA in its default install namespace,
  `openshift-lifecycle-agent`, as in the telco reference configuration.
- A blocked IBGU never resumes on its own (CNF-30061): after fixing the cause, delete and
  recreate it. An IBGU whose selector matches nothing reports a block, not success — a
  behaviour change from `main`, where it completed.
- Only one IBGU may act on a cluster at a time. This is assumed, not enforced, until
  CNF-29901.
- The spoke-side seed check requires hub ACM 2.14 or later: it relies on LCA's claim being
  exempt from the custom-claim cap, and has no fallback read.

## Open items that affect the test plan

| Item | Owner | Impact on QE |
|---|---|---|
| Backport scope (which z-streams) | LCA / product (CNF-28772) | upgrade matrix to test |
| `oadpContent` removal release | LCA | future; not in scope now |

## Appendix A — Sample manifests

Content follows the RAN reference configuration in `openshift-kni/telco-reference`
(`main`). Today the reference IBU policies point `oadpContent` at ConfigMap `oadp-cm` in
`openshift-adp` (`telco-ran/configuration/acmpolicygenerator/image-based-upgrades/pgt-ibu-upgrade.yaml`),
and that ConfigMap carries the `acm-klusterlet` Velero Backup/Restore pair from
`telco-ran/configuration/source-crs/ibu/PlatformBackupRestoreWithIBGU.yaml`. The samples
below express that same content with the new API, named `acm-klusterlet` after the Velero
CRs, so test fixtures map one to one onto today's reference.

**Names.** `BackupContent` is the kind on both hub (`lcm.openshift.io/v1alpha1`, TALM)
and spoke (`lca.openshift.io/v1`, LCA); `backupContent` is the IBU field. The field layout
inside a BackupContent follows CNF-26659's sketch. The namespace `ibu-4-20-to-4-22`
stands for one upgrade path: the curated BackupContent entries for that path and the
IBGUs that use them live together there.

### A.1 Today: an IBGU using `oadpContent` (unchanged by this release)

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: ImageBasedGroupUpgrade
metadata:
  name: ibu-4-22-oadp
  namespace: ibu-4-20-to-4-22
spec:
  clusterLabelSelectors:
    - matchLabels:
        group-du-sno: ""
  ibuSpec:
    seedImageRef:
      image: registry.example.com/lca-seed:4.22.3
      version: 4.22.3
      pullSecretRef:
        name: seed-pull-secret
    oadpContent:                       # the ConfigMap must exist in openshift-adp on the hub
      - name: oadp-cm
        namespace: openshift-adp
  plan:
    - actions: ["Prep"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 240}
    - actions: ["Upgrade"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 240}
    - actions: ["FinalizeUpgrade"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 30}
```

### A.2 Hub: the curated backup definition (TALM `BackupContent`)

Same content as the reference `acm-klusterlet` Backup: the objects listed in its
`lca.openshift.io/apply-label` annotation become per-kind `names`, and the Restore's
`lca.openshift.io/apply-wave: "1"` becomes `restoreWave: 1`. `backupWave` is omitted,
so it is processed in the last backup wave.

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: BackupContent
metadata:
  name: acm-klusterlet                 # the hub entry name, and the spoke object's name
  namespace: ibu-4-20-to-4-22          # the upgrade path; must be the IBGU's namespace
spec:                                  # LCA's exported BackupContentSpec, unwrapped
  restoreWave: 1
  namespaces:
    - open-cluster-management-agent
  namespacedResources:
    - kind: deployments.apps
      names: [klusterlet]
    - kind: serviceaccounts
      names: [klusterlet]
    - kind: secrets
      names:
        - bootstrap-hub-kubeconfig
        - open-cluster-management-image-pull-credentials
  clusterScopedResources:
    - kind: klusterlets.operator.open-cluster-management.io
      names: [klusterlet]
    - kind: customresourcedefinitions.apiextensions.k8s.io
      names: [klusterlets.operator.open-cluster-management.io]
    - kind: clusterroles.rbac.authorization.k8s.io
      names:
        - klusterlet
        - open-cluster-management:klusterlet-work:ibu-role
        - open-cluster-management:klusterlet-admin-aggregate-clusterrole
    - kind: clusterrolebindings.rbac.authorization.k8s.io
      names: [klusterlet]
    - kind: priorityclasses.scheduling.k8s.io
      names: [klusterlet-critical]
```

The hub `spec` is exactly LCA's `BackupContentSpec`, with no wrapper, so this object and
the spoke copy in A.4 differ only in `apiVersion` and namespace. `spec` is immutable: to
change the content, create a new entry (and a new IBGU).

### A.3 Hub: the new IBGU using `backupContent`

No new IBGU field: the list goes in LCA's own field inside `ibuSpec`, naming hub entries
in the IBGU's namespace.

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: ImageBasedGroupUpgrade
metadata:
  name: ibu-4-22
  namespace: ibu-4-20-to-4-22
spec:
  clusterLabelSelectors:
    - matchLabels:
        group-du-sno: ""
  ibuSpec:
    seedImageRef:
      image: registry.example.com/lca-seed:4.22.3   # must be a new-format seed (safety rule)
      version: 4.22.3
      pullSecretRef:
        name: seed-pull-secret         # also used by TALM to read the seed's format label
    backupContent:                     # names of BackupContent entries in this namespace
      - name: acm-klusterlet
  plan:
    - actions: ["Prep"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 240}
    - actions: ["Upgrade"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 240}
    - actions: ["FinalizeUpgrade"]
      rolloutStrategy: {maxConcurrency: 200, timeout: 30}
```

### A.4 Spoke: the LCA `BackupContent` TALM copies (in the Prep manifestwork)

```yaml
apiVersion: lca.openshift.io/v1
kind: BackupContent
metadata:
  name: acm-klusterlet                 # same as the hub entry
  namespace: openshift-lifecycle-agent # ibuv1.DefaultNamespace, LCA's default install namespace; the hub namespace is not carried
spec:
  restoreWave: 1
  namespaces:
    - open-cluster-management-agent
  # ... identical to A.2 spec ...
```

No `ownerReferences`, `uid`, `resourceVersion` or hub labels. It stays on the spoke after
the Prep manifestwork is deleted (`Orphan`) and is read again by LCA at Upgrade pre-pivot.

### A.5 Spoke: the IBU TALM ships for Prep

```yaml
apiVersion: lca.openshift.io/v1
kind: ImageBasedUpgrade
metadata:
  name: upgrade
spec:
  stage: Prep
  seedImageRef:
    image: registry.example.com/lca-seed:4.22.3
    version: 4.22.3
    pullSecretRef:
      name: seed-pull-secret
  backupContent:                       # unchanged from the IBGU's ibuSpec
    - name: acm-klusterlet
```

After a successful Prep, `PrepCompleted`'s message names the validated BackupContent
(`acm-klusterlet`) (CNF-28671).

### A.6 Negative samples

Both inputs set — rejected at IBGU admission (the mutual-exclusion rule inherited from
LCA's spec type):

```yaml
  ibuSpec:
    seedImageRef: {image: registry.example.com/lca-seed:4.22.3, version: 4.22.3}
    oadpContent:
      - {name: oadp-cm, namespace: openshift-adp}
    backupContent:
      - name: acm-klusterlet
```

`backupContent` with a pre-bump seed — rejected by the IBGU pre-flight safety rule before
any CGU is created (the same IBU applied to a pre-bump spoke by a policy would be pruned
silently and upgrade with no application backup):

```yaml
  ibuSpec:
    seedImageRef:
      image: registry.example.com/lca-seed:4.22.3-old-lca-cli   # built with a pre-bump lca-cli; label carries the old format
      version: 4.22.3
      pullSecretRef: {name: seed-pull-secret}
    backupContent:
      - name: acm-klusterlet
```

Reference to a missing hub entry — IBGU blocked before any CGU, condition names
`acm-klusterlet-typo`:

```yaml
    backupContent:
      - name: acm-klusterlet-typo
```

## Tickets

- Epic: CNF-26657. Spike (hub design): CNF-28677.
- LCA: CNF-22167 / PR#7818, CNF-26659 (API, PR #10403), CNF-29691 (engine integration), CNF-28671, CNF-28751, CNF-28926; backport and
  migration guide CNF-28772; reference CRs CNF-28676; upstream docs CNF-28679; downstream
  docs TELCODOCS-3009.
- TALM: CNF-29239 – CNF-29245, CNF-29247, CNF-30039, CNF-30061, CNF-30176; future improvements under CNF-6791: CNF-29281, CNF-29901, CNF-29246, CNF-30062, CNF-30139.
