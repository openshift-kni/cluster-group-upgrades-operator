# IBGU / IBU v2 — Hub-Side Design Options

Working document for the TALM (hub) side of CNF-26657 "Remove OADP requirement".
Scope: everything that changes in `openshift-kni/cluster-group-upgrades-operator`
because LCA replaces `spec.oadpContent` with a BackupSpec reference list.

**Status of inputs.** For this exercise, two things that CNF-26659 currently records
as settled are treated as **open**:

- whether the IBU API moves to `v2` or absorbs the change additively in `v1`
- whether BackupSpec is cluster-scoped or namespaced, on the spoke and on the hub

Everything else from CNF-26659 is taken as given: BackupSpec is LCA-owned, inert
(no controller, no status subresource), validated only during IBU Prep, with no
runtime translation of Velero-format ConfigMaps.

Code facts cited below are verified against this repo at `main`; see the appendix.

---

## Decision map

```
D1  IBGU API shape across LCA generations      <- the structural decision
 |
 +- D2  Who owns cluster-list resolution        <- independent; NOT required by D4
 |
 +- D3  How the hub learns a spoke's IBU generation
 |   +- D4  What the hub does on mismatch / unknown
 |
 +- D5  Who delivers BackupSpec CRs to the spoke
     +- D6  BackupSpec scoping (spoke / hub)         [moot if policy delivers]
         +- D7  Where the hub reads the content from
             +- D8  Copy semantics and lifecycle on the spoke
                 +- D9  Behaviour when a reference is unresolvable

D10 Status-feedback robustness       <- independent, can land immediately
D11 Release sequencing               <- depends on D1
```

D1 and D5 are the two that actually need a decision meeting. D2 is independently
worth doing. The rest follow.

---

## D1. How does IBGU express the IBU spec across LCA generations?

**Context.** `ImageBasedGroupUpgrade.spec.ibuSpec` embeds `lcav1.ImageBasedUpgradeSpec`
wholesale, is required, and is immutable via CEL `self == oldSelf`
(`pkg/api/.../types.go:33`). `createIBU` stamps it verbatim into the IBU shipped to the
spoke (`controllers/imagebasedgroupupgrade.go:258`). So the IBGU CRD is a mirror of
LCA's spec, and moves whenever LCA's spec moves.

The requirement driving this decision: **one TALM build must serve a fleet containing
both LCA generations simultaneously**, via separate IBGU CRs per generation. Mixed
generations inside a single IBGU is explicitly not wanted — too confusing to manage.

### Option 1A — Additive field in LCA v1, single IBGU version

LCA keeps `oadpContent` (deprecated) and adds the BackupSpec ref list in the same
`lca.openshift.io/v1` spec. IBGU vendors the newer LCA and regenerates. One schema
accepts both shapes; each IBGU CR fills in whichever its fleet needs.

| Pros | Cons |
|---|---|
| Cheapest by a wide margin — regenerate, branch `getConfigMapManifests`, add CEL exactly-one-of | **Depends entirely on an LCA decision TALM does not own** |
| No conversion, no webhook, no new kind, no migration | Mis-targeting is silent in **one** direction only, and the other is LCA's choice — see the note below |
| IBU apiVersion stable, so `ibuToBytes` and the work-agent ClusterRole are untouched | Deprecated field lingers in the schema indefinitely |
| Existing IBGU objects stay valid | `ibuSpec` can't enforce "exactly one shape" as strongly as a dedicated schema |

### Two variants of 1A, and they are not close

**1A(i) — schema-only compatibility.** `oadpContent` stays in the schema but is rejected
at Prep once the BackupSpec path is in use. A newer spoke will not perform an old-style
backup. TALM still has to send the old shape to old spokes and the new shape to new
ones, so the hub keeps the full mixed-fleet burden; what it gains is that one schema
expresses both, so no second field, version or kind is needed.

**1A(ii) — functional compatibility.** LCA keeps *honouring* `oadpContent` as a
deprecated input alongside BackupSpec. This is the stronger option, and it changes the
shape of the whole problem:

- **It genuinely justifies staying on `v1`.** A version bump is warranted when the
  contract breaks. If the old input keeps working, nothing breaks and the change is
  purely additive. The entire v2 cascade then evaporates across both repos — conversion
  strategy, webhook-or-not, the `None`-pruning hazard, dual-field or dual-kind hub APIs,
  IBGU object migration, and the hub/spoke version matrix.
- **It removes the API axis of the mixed-fleet problem — but not all of it.** If a newer
  spoke honours both shapes, TALM can send `oadpContent` to *any* spoke, so the "one
  TALM build must serve both IBU generations" requirement disappears and D1 reduces to
  "change nothing until you want BackupSpec". Only new-shape → old-spoke stays
  hazardous, and that is avoidable by not adopting BackupSpec until the fleet supports
  it — operator policy rather than enforcement. **The seed format axis is untouched**;
  see below.
- **It de-risks the migration guide.** CNF-28772's Wave 2 forcing-function pain — ZTP
  and ACM policies going NonCompliant or flapping until the customer's git repo is
  updated, IBGU objects needing delete-and-recreate — exists only because migration is
  compulsory at an LCA upgrade. Customer-paced migration removes all of it.
- **It keeps the LCA rollback path intact.** CNF-28772 flags operator rollback as a
  trap; supporting both inputs means rolling LCA back does not strand backup content.

**The main objection to 1A(ii) is weaker than it looks.** "LCA would have to maintain two
backup engines" is not accurate: PR#7818 removed the *OADP operator* dependency while
**keeping the Velero CR format** as its input. So the old path is already LCA's own
engine reading Velero-format content, and it ships today. 1A(ii) is largely a decision
*not to delete* existing, shipping code, rather than to build or maintain a second
implementation. CNF-26659's settled position ("LCA never reads the Velero format at
runtime in v2") is what makes this a deletion; reversing it is cheaper than the phrasing
suggests.

### What 1A(ii) does not fix: the seed format axis

PR#7818 bumps `SeedFormatVersion`, and that check is a hard equality during Prep between
the seed image's OCI label and the source cluster's compiled-in constant. It is
**independent of the IBU API version and of whether `oadpContent` is honoured** — it
ships with the OADP removal itself, in the earlier wave. Because `seedImageRef` lives
inside the fleet-wide, immutable `ibuSpec`, one IBGU commits to exactly one seed image
and therefore to one seed format version, so during the LCA rollout window an IBGU
spanning both halves of the fleet fails Prep on whichever half mismatches. (Recorded on
CNF-28772.)

So under 1A(ii) the hub still needs per-spoke capability detection (D3) and a pre-flight
check (D4). What changes is their character, not their necessity:

| Axis | Failure mode | Pre-flight is |
|---|---|---|
| IBU API generation | silent prune, no backup, Prep reports success | a **safety** requirement — and 1A(ii) removes this axis entirely |
| Seed format version | LCA reports the stage failed; surfaces as `<Stage>Completed=False` (D10) | an **efficiency** requirement — it avoids a wasted Prep cycle per cluster and the batch stalls that follow |

Detecting the seed axis needs one thing the API axis does not: the hub must know the
*seed image's* format version, not only the spoke's expectation. Options:

- **Inspect the OCI label** `com.openshift.lifecycle-agent.seed_format_version` on the
  seed image from the hub, using the pull secret TALM already reads from the IBGU
  namespace (`imagebasedgroupupgrade.go:408`). Most accurate, but it requires registry
  egress from the hub, which TALM does not do today.
- **Declare it on the IBGU** as an explicit expected-seed-format field. No new
  capability needed, and a wrong declaration is still caught, because it would mismatch
  every spoke rather than passing silently.
- **No detection** — let Prep fail. Loud, but costs a Prep cycle per cluster plus the
  batch stalls described in D4.

The declared-field option is the cheap 80% and is probably where to start; registry
inspection is better UX but a larger step for the hub.

**The real cost of 1A(ii)** is that a deprecation with no forcing function does not
complete. Customers who are never obliged to migrate largely will not, and the old input
gets carried for years. So this defers the breaking change rather than avoiding it — the
eventual *removal* of `oadpContent` is the real v2. That is usually the right trade
(by then the new path is proven and most of the fleet has moved), but it should be an
explicit decision with a declared removal release, not a drift. Secondary costs: two
validation paths for CNF-28671 to cover, and a rule for when both fields are set —
reject as exactly-one-of rather than defining precedence.

**Mis-targeting under 1A(i) is asymmetric, and only half of it is inherent.** Because the
schema is additive, the two wrong-shape cases behave quite differently:

- **New ref list → older spoke.** The older CRD has no such field, so it is pruned on
  arrival. LCA sees an IBU with no backup content and no indication that any was
  intended, and Prep proceeds. **The spoke cannot detect this**, so nothing on the spoke
  side can be specified to catch it. Irreducibly silent; only the hub-side pre-flight
  (D3/D4) can prevent it.
- **Deprecated `oadpContent` → newer spoke.** The field is *still in the schema*, so it
  is **not** pruned. LCA receives it and can see exactly what the operator intended.
  What happens next is a behaviour LCA gets to define, not a property of the API.

That second case is worth specifying explicitly rather than leaving to chance: LCA
should **reject a populated `oadpContent` during Prep** once the BackupSpec path is in
use. That converts the mistake into an LCA-reported stage failure, which surfaces as
`PrepCompleted=False` and is promptly detectable by the hub (see D10). Silently ignoring
the field would be the worst choice available, since the operator's intent is right
there in the object.

Note this makes 1A a schema-compatibility measure, not a functional back-compat one: a
newer spoke will not perform an old-style backup, and CNF-26659 has already settled that
LCA never reads the Velero format at runtime. TALM still sends the old shape to old
spokes and the new shape to new ones. What 1A buys the hub is that **one schema can
express both**, so no second field, version or kind is needed — not that either spoke
accepts both.

**Verdict.** Not actionable unilaterally — both variants depend on an LCA decision TALM
does not own — but worth pushing for before committing to anything else here.

**1A(ii) dominates every other option in this section**, because it removes the API
coexistence requirement that drives all of them rather than finding a cheaper way to
satisfy it. If it is achievable, the rest of D1 is moot — but D3 and D4 are not, because
the seed format axis survives it.

**1A(i)** is a much smaller win: it saves the hub a second field/version/kind, but the
mixed-fleet burden stays and one mis-targeting direction remains irreducibly silent, so
D3/D4 stay load-bearing. Still better than any option that removes the old field
outright — under 1C/1D a newer spoke cannot even tell `oadpContent` was intended,
because the field does not exist in its schema to be seen.

### Option 1B — Sibling fields in IBGU `v1alpha1`

Keep `ibuSpec` (lcav1, deprecated), add `ibuSpecV2` (lcav2). CEL enforces exactly one.
`ibuSpec` drops `Required` — a safe widening, since every existing object sets it.

| Pros | Cons |
|---|---|
| No conversion, no webhook, no new CRD | Two fields describing one concept is an ugly API |
| Works regardless of what LCA decides | Neither field can be `Required`; exactly-one-of via CEL is weaker than a schema that just demands the right thing |
| Existing objects stay valid; per-field immutability unaffected | The deprecated field is permanent in `v1alpha1` |
| Bounded mechanical cost: ~7 functions typed on `*lcav1.ImageBasedUpgrade` need an interface or a parallel set | Schema can't be tightened per generation |

**Verdict.** The pragmatic choice if coexistence is short (one or two releases).

### Option 1C — New IBGU API version (`v1alpha2`), same kind

| Pros | Cons |
|---|---|
| Conventional signal that the API changed | **`None` conversion strategy is unsafe here.** K8s treats `None` as appropriate only when schemas are effectively identical; with differing schemas the differing field does not survive. A `v1alpha1` IBGU carrying `oadpContent` against a `v1alpha2` storage version loses it — TALM then emits an IBU with no backup content and Prep reports success. Silent no-backup, manufactured by TALM's own API |
| No permanently deprecated field | So a **conversion webhook is forced** — and it has nothing useful to do. It cannot synthesise BackupSpec refs from a ConfigMap ref (CNF-26659 settles that translation is an offline customer activity), so the conversion is either lossy (reintroducing the silent drop) or refusing (breaking `kubectl get ibgu` on mixed lists) |
| OLM provides conversion-webhook cert generation and CA injection, so infra cost is lower than it first appears (CSV entry + webhook server, not cert-manager from scratch) | Hub-wide availability dependency: webhook down ⇒ IBGU reads fail across the fleet |
| | **Wrong modelling axis.** API versions give clients different *views of one object*. Here the two shapes describe *different fleets* — distinct objects that coexist. Which generation an IBGU targets becomes a property of how you requested it rather than of the object |

**Verdict.** Not recommended. The version bump buys nothing a field or a kind doesn't,
and costs a webhook plus a new silent-data-loss path.

### Option 1D — New kind (e.g. `ImageBasedGroupUpgradeV2`)

Two distinct CRDs, one version each. Kubernetes never converts between them.

| Pros | Cons |
|---|---|
| **No conversion at all** — the entire `None`/webhook problem chain disappears | Version number in a kind name cuts against API convention and ages badly at a third generation; there is no obvious semantic alternative name |
| Targeting is encoded in the object's identity, which is where it belongs | Duplicated surface: two CRDs, CSV entries, RBAC blocks, sample sets, doc sections, plus anything grepping `kind: ImageBasedGroupUpgrade` (incl. CNF-28676's `PlatformBackupRestoreWithIBGU.yaml`) |
| **Each schema can be tight** — genuinely required fields and generation-specific CEL, rather than a weak exactly-one-of | **Identity collision to design around (concrete):** CGUs are claimed by the single label `ibgu: <name>` with no kind discriminator (`constants.go:185`, used at `imagebasedgroupupgrade.go:198,377`), and `getCGUNameForPlanItem` (`:253`) derives CGU names as `<ibgu-name>-<actions>-<index>`. Two kinds sharing an object name in a namespace would generate identical CGU names and adopt each other's CGUs. Same risk for the `lcm.openshift.io/ibgu-<action>-*` ManagedCluster labels and their regex cleanup |
| Clean removal: delete the whole CRD when the old generation is gone, no storage migration | Controller duplication — though smaller than it looks: plan progression, CGU creation, status sync and cluster labelling are identical; only `createIBU` (`:258`) and `getConfigMapManifests` (`:437`) differ. One reconciler over an interface covers it |

**Verdict.** The strongest option if coexistence is long-lived.

### Option 1E — Decoupled `ibuSpec` (passthrough)

Replace the embedded LCA type with `RawExtension` / `x-kubernetes-preserve-unknown-fields`,
or with TALM's own hand-maintained shape. IBGU stops mirroring LCA permanently.

| Pros | Cons |
|---|---|
| Solves this problem *and every future one* — IBGU decouples from LCA's version cadence | Passthrough gives up hub-side schema validation on `ibuSpec` entirely. Typos land silently and get pruned on the spoke — precisely the failure class this whole effort is trying to eliminate |
| A genuine reason to spend a version bump, unlike 1C | A hand-maintained parallel schema avoids that but is a permanent maintenance tax and drifts from LCA |
| Only option that survives LCA v3 with no hub change | Weakest `kubectl explain` / UX story |

**Verdict.** Worth a serious look only if the team wants IBGU to stop tracking LCA
structurally. Otherwise the validation loss is too expensive.

### Option 1F — No coexistence (TALM release N serves LCA generation N only)

| Pros | Cons |
|---|---|
| Zero API work; mirrors what CNF-26659 does for LCA itself | Fails the stated requirement — a hub managing a mixed fleet cannot upgrade |
| Simplest possible mental model | Forces hub/fleet lockstep, unrealistic for telco EUS fleets where 4.16 is still in the field |

**Verdict.** Listed as the baseline. Not viable given the requirement.

### D1 recommendation

**Settle 1A(ii) first — it is the only option that removes the API coexistence problem
instead of managing it.** If LCA keeps honouring `oadpContent` as a deprecated input,
the requirement that drives 1B through 1E disappears and the rest of this section is
moot. It is also the option that best justifies LCA staying on `v1`, which CNF-26659 is
still holding open. Pair it with a declared removal release so the deprecation actually
completes.

**It does not, however, make D3 and D4 optional.** The seed format version bump ships
with the OADP removal regardless of what happens to the IBU API, and `seedImageRef` is
fleet-wide and immutable, so a mixed-fleet split is required during the LCA rollout
window either way. 1A(ii) downgrades the pre-flight check from a safety requirement to
an efficiency one; it does not remove it.

**If 1A(ii) is rejected, the deciding question becomes how long the two generations must
coexist:**

- **Short (1–2 releases):** Option 1B (sibling fields). Lowest cost, no new machinery.
- **Long (telco EUS lifetimes — likely, since 4.16 is still in field use):** Option 1D
  (new kind). Conversion-free, keeps per-generation schema validation, and deletes
  cleanly when the old generation retires.
- **1A(i)** is worth taking alongside either, since it costs LCA little and makes one
  mis-targeting direction loud.
- Rule out 1C. Treat 1E as a separate strategic conversation.

So: push 1A(ii), fall back to **1D** on the assumption of long coexistence.

---

## D2. Who owns cluster-list resolution?

**Context.** Today IBGU resolves nothing. `ensureCGUForPlanItem` passes
`clusterLabelSelectors` straight through to each generated CGU
(`controllers/utils/manifestworkreplicaset.go:335`), narrowed per plan item by
`getLabelSelectorForPlanItem` (`:264`), which appends `ibgu-<action>-completed`
requirements — Abort targets clusters *without* upgrade-completed, Rollback and
FinalizeUpgrade those *with* it, FinalizeRollback those with rollback-completed. Each
CGU then resolves that selector independently, and IBGU learns the resulting set only
afterwards by reading `cgu.Status.Clusters` in `syncStatusWithCGUs`.

Two consequences, both accepted as simplifications for IBGU v1:

- **Independent resolution per plan item.** Different plan items can act on different
  cluster sets.
- **A live race, not merely drift.** The per-plan-item narrowing depends on
  ManagedCluster labels that IBGU itself writes in `ensureClusterLabels`
  (`imagebasedgroupupgrade.go:126`), which collects `failedClusters` and can partially
  fail. A later CGU can therefore resolve against stale or partially-written labels. The
  routing path today is `IBGU status → ManagedCluster labels → CGU selector → CGU
  re-resolves → cluster set`, with a write that can fail in the middle of it.

**The full set of predicates** (`getLabelSelectorForPlanItem`, keyed on
`planItem.Actions[0]` only, so a composite item like `['Prep','Upgrade']` falls through
to `default` and keeps the base selector unchanged):

| Action | Narrowing |
|---|---|
| `Prep` (and any composite starting with it) | none — base selector |
| `Upgrade` | `ibgu-prep-completed` exists |
| `Abort` | `ibgu-upgrade-completed` does **not** exist |
| `Rollback`, `FinalizeUpgrade` | `ibgu-upgrade-completed` exists |
| `FinalizeRollback` | `ibgu-rollback-completed` exists |
| `AbortOnFailure` | **two** selectors, OR'd: `ibgu-prep-failed`, or `ibgu-upgrade-failed` |

This decision is largely independent of D1. It is **not** a prerequisite for the D4
pre-flight check (see D4), and could land on its own.

| Option | Pros | Cons |
|---|---|---|
| **2A** Status quo — pass selectors through, each CGU resolves independently | No work; no new IBGU responsibility | Keeps the per-plan-item drift and the label-write race; gives D4 nothing to validate against |
| **2B** IBGU resolves once, passes explicit `spec.clusters` to each generated CGU | Single resolution point; deterministic set across the whole plan; the set D4 validates is exactly the set that runs | IBGU takes on a responsibility it does not have today; must handle vanished clusters (below); **does not remove the label write from the control path**, because eligibility still has to be evaluated from ManagedCluster labels (see "rejected" below) |
| **2C** Create CGUs disabled, let CGU resolve, read back, validate, then enable | Reuses CGU's canonical resolution; no duplicated resolver | **Does not actually help.** Each plan item's CGU still resolves independently, so the drift is carried forward across actions rather than removed. Also does not work as-is: with `enable: false` the resolved list never reaches status — `Status.Clusters` is only appended during remediation (`clustergroupupgrade_controller.go:808,825`) and `Status.RemediationPlan` is built at `:1120`, after the enable gate returns at `:383`. Would require a CGU change to persist the selected set at `ClustersSelected` time |

### D2 recommendation

**2B — IBGU resolves once and hands each generated CGU an explicit cluster list.**

Design points:

- **Resolve the base selector only.** `getLabelSelectorForPlanItem` only ever *narrows*,
  so the base set is a conservative superset of every plan item's set. One resolution
  covers the whole plan.
- **Freeze at plan item 0, and let plan item 0's CGU `spec.clusters` be the record.**
  No separate copy in IBGU status. Size is not a constraint: a few thousand cluster
  names is ~100–200 KB against a ~1.5 MB object limit, and `maxConcurrency` already
  auto-clamps to the cluster count (`clustergroupupgrade_controller.go:1367-1372`).
- **Evaluate per-plan-item eligibility from ManagedCluster labels, in IBGU.** Apply the
  predicate table above against the frozen set rather than handing the selector to the
  CGU. The labels stay load-bearing — see the rejected approach below for why they
  cannot be replaced by IBGU's own status.
- **Intersect with "still exists as a ManagedCluster" on every plan item.** This is the
  one real risk of an explicit list: `validateCR` hard-fails when any listed cluster is
  not a ManagedCluster (`:1345-1347` → `ClustersSelected=False`, `ClusterNotFound`, CGU
  stops), whereas a selector simply stops matching a decommissioned spoke. Without the
  intersection, deleting one spoke mid-rollout would block the remainder of the IBGU.
  Record dropped clusters in IBGU status so the removal is visible rather than silent.
- **RBAC is already sufficient** — `managedclusters` get/list/watch/patch/update
  (`config/rbac/role.yaml:63-69`).
- Canaries are unused by IBGU, so the canary validation at `:1350` stays a no-op.

### Rejected: deriving eligibility from `ibgu.Status.Clusters`

Tempting, since `Status.Clusters` carries `CompletedActions`/`FailedActions` and is the
same data `ensureClusterLabels` consumes. **It does not work.** Recorded here so it is
not re-proposed.

1. **Cross-IBGU plans break outright.** The plan CEL admits single-item plans —
   `[['Upgrade']]`, `[['FinalizeUpgrade']]`, `[['Rollback']]`, `[['FinalizeRollback']]`,
   `[['Abort']]`. Each narrows on a label written by a *previous* IBGU, and a follow-up
   IBGU's own status is empty at that point, so a status-derived predicate selects
   nothing. This is documented, intended workflow: "In case there are clusters that have
   failed `Prep` or `Upgrade`, user can create a separate IBGU with `Abort` action…"
   (`docs/ibgu/README.md:193,195`). ManagedCluster labels are **deliberately** the
   cross-IBGU carrier of state.

2. **Labels have reset semantics that status does not.** `ensureClusterLabels` wipes
   *every* `lcm.openshift.io/ibgu-*-(completed|failed)` label once a cluster completes
   Abort, AbortOnFailure, FinalizeUpgrade or FinalizeRollback
   (`imagebasedgroupupgrade.go:134-138`), while `Status.Clusters` keeps the cumulative
   history. Concrete divergence in the valid plan
   `[['Prep'],['AbortOnFailure'],['Upgrade'],['AbortOnFailure']]`: a cluster that fails
   Prep and is aborted at item 1 has its labels wiped, so item 3's AbortOnFailure
   correctly excludes it — but a status predicate still sees `Prep` in `FailedActions`
   and would re-abort it.

Reproducing (2) from status is possible — "actions since the last reset action", relying
on `CompletedActions` ordering from `SortCGUListByPlanIndex` — but (1) is not
reproducible at all. Labels stay authoritative.

---

## D3. How does the hub learn which IBU generation a spoke speaks?

Needed by every D1 option except 1F. This is new machinery whichever way it goes: TALM
reads **no** spoke version or capability information today — no `openshiftVersion`, no
ClusterClaims, no ManagedClusterInfo anywhere in the tree.

**The real question is which side owns the mapping.** If the hub infers capability from
a version, it must carry a table of "LCA version X supports Y" — which encodes another
team's release and backport decisions as constants inside TALM, to be re-shipped
whenever those decisions change. If the spoke declares its own capabilities, the
component that owns the contract publishes it and the hub simply reads. The second is
the better coupling, and it generalises to any future LCA contract change rather than
being specific to this one. It is the runtime sibling of CNF-28751, which does the same
thing at compile time for condition type names.

| Option | Pros | Cons |
|---|---|---|
| **3A** ManagedCluster label set by ZTP/ACM | Cheap; no new API calls; already how TALM filters clusters | Trusts out-of-band tooling; stale or missing on hand-registered clusters; nothing validates it against reality |
| **3B** ManagedClusterInfo / addon status | Authoritative-ish; already populated by ACM | Depends on what ACM actually surfaces about LCA; may not carry operator version |
| **3C** ManagedClusterView on the LCA CSV (or the IBU CRD's served versions) | Ground truth, read from the spoke itself; TALM already has View helpers in `utils/multicloud_util.go` | New per-cluster API objects and a reconcile wait before the plan can start; cost scales with fleet size; the hub still has to map a version to a capability |
| **3D** No detection — trust the user's label selector | Zero cost | The failure it permits is a silent no-backup upgrade. Unacceptable on its own |
| **3E** Dry-run probe ManifestWork | Tests the real apply path | Heavyweight; another round trip; still needs somewhere to report the result |
| **3F** LCA publishes its capabilities as a ClusterClaim | The idiomatic ACM mechanism for a spoke to assert a fact to the hub. Surfaces as `ManagedCluster.status.clusterClaims`, so the hub reads the whole fleet in **one List** — no per-cluster objects, no reconcile wait. Type is already vendored (`vendor/open-cluster-management.io/api/cluster/v1/types.go:168,188`) and TALM already holds `managedclusters` get/list/watch, so the hub side costs nothing new. Removes the version→capability table entirely and serves every future contract change | Requires an LCA change, so not unilateral. **Cannot be retrofitted into already-shipped LCA versions**, so "claim absent" stays ambiguous between a pre-feature LCA and broken claim sync. Becomes a public contract LCA must then maintain |

**Recommendation: 3F, with 3C as the fallback** for clusters whose LCA predates the
claim. Never 3D alone.

Design notes:

- **Publish capabilities, not a version.** A published version number just relocates the
  mapping table to the hub and defeats the point.
- **This is an input to the check, not the check.** It tells the hub what the spoke
  supports; D4 decides what to do with that.
- **Absent and stale must both count as unknown.** Claims are spoke-reported and synced,
  so an unreachable spoke yields no usable answer and has to be treated accordingly.
- **Land it as early as possible.** The "claim absent" boundary falls wherever the claim
  first ships, and every LCA version released before that point permanently needs the
  3C fallback. The earlier it lands, the narrower the band that needs one — and that
  band never shrinks afterwards.
- **Mind the custom-claim cap — this one can bite hard.** OCM caps *custom* cluster
  claims per spoke at `max-custom-cluster-claims`, default **20**. Past that, the agent
  sorts custom claims **by name** and truncates to the limit
  (`ocm/pkg/registration/spoke/managedcluster/claim_reconcile.go:115-124`). Three
  consequences:
  - Which claims survive depends on alphabetical name order, so LCA's claim competes
    with every other operator's on a spoke, and can be dropped by an unrelated operator
    being installed.
  - The `CustomClusterClaimsTruncated` event is recorded **on the spoke**, so the hub
    sees nothing. A truncated claim is indistinguishable from an absent one.
  - Combined with fail-closed (D4), that is a blocked fleet upgrade whose cause is
    invisible from the hub.

  **Mitigation:** claims matching a reserved name or reserved suffix are exempt from the
  cap entirely (`:95-101`). LCA's claim should use one. The reserved suffix list is
  configured on the spoke registration agent (`--reserved-cluster-claim-suffixes`), so
  this is not LCA's call alone — **confirm what ACM reserves by default** before relying
  on 3F. If LCA's claim cannot be made reserved, 3F inherits a silent failure mode and
  3C becomes materially more attractive.

### Future: selecting by claim rather than verifying with it

Claims are invisible to a label selector, and OCM has no claim-to-label sync — the spoke
agent writes claims only to `ManagedCluster.status.clusterClaims`
(`claim_reconcile.go:124`). OCM's own answer to claim-based selection is `Placement`,
whose `requiredClusterSelector` carries a `claimSelector` beside the `labelSelector`
(`vendor/open-cluster-management.io/api/cluster/v1beta1/types_placement.go:146,232`).
IBGU cannot use it today: CGU selects only by `clusters` + `clusterLabelSelectors`, and
TALM's existing Placement use runs the other way — `SetPlacementClusterNames`
(`controllers/utils/policy_util.go:269`) pins an explicit name list into a Placement, so
Placement is an output of selection, not an input. There is also no `placementdecisions`
RBAC today.

**Deferred, and deliberately not the answer to D3.** Selection and verification want
different properties: selection should be user-authored, reviewable in git and
predictable; verification should be authoritative and independent of what the author
believed. If the claim drove selection, a mis-targeted IBGU would be impossible — but so
would the independent check, and one mechanism would be asserting its own correctness.
The operator expressing intent via a selector, and the system verifying it against
spoke-reported ground truth, is the safer arrangement.

Worth revisiting later purely as a UX improvement (it would generalise to any claim, not
just LCA capabilities). It would need a new immutable IBGU field, PlacementDecision
reading, and new RBAC, and it interacts with D2 — a resolved Placement fits option 2B
directly.

There is also a hard safety gate on it, from the truncation cap above. Selecting by
claim is strictly more dangerous than verifying by claim, because the two failure shapes
are not comparable:

- **Verifying** with a truncated or absent claim reads as "unknown", and fail-closed
  (D4) turns that into a blocked upgrade with a visible condition. Noisy, safe,
  diagnosable.
- **Selecting** with a truncated claim means the cluster silently fails to match. Not
  blocked, not reported, not in the fleet — the IBGU runs to green over a smaller set
  than the operator intended, and nothing says so. Since truncation is alphabetical and
  the event lands on the spoke, a spoke can drop out of a fleet because an unrelated
  operator was installed on it, and the hub still reports success.

So: **if the claim is truncatable, use it to verify, not to select.** Selection by claim
should not be considered at all unless LCA's claim is exempt from the cap, and even then
it should narrow a label-defined fleet rather than replace one, so the intended set
keeps a hub-side definition that does not depend on claim delivery.

Note the asymmetry that makes this matter: a v2 IBU sent to a v1 spoke fails loudly
(no matching kind), but a v1 IBU sent to a v2 spoke may be *accepted* during the release
where LCA still serves v1 for conversion — then pruned to v2 at rest, leaving LCA to read
an IBU with no backup content. That direction is silent, and TALM is the most likely
thing to produce it.

---

## D4. What does the hub do on mismatch, or when the generation is unknown?

**The rule itself is not a D4 choice.** TALM must only ever send an IBU at the generation
the spoke actually speaks, and *both* directions violate that:

- **v2 IBU → v1 spoke** — the kind does not exist and the apply is rejected.
- **v1 IBU → v2 spoke** — accepted, because v1 stays served through the conversion
  release, then pruned at rest. LCA reads v2, sees no backup content, and Prep succeeds
  having taken no backup.

D4 governs only the *response to uncertainty*: what to do when detection reports a
mismatch, or cannot establish the generation at all. "Fail open" never means "send it
anyway."

### Both directions must be checked, and checked before the rollout starts

It is tempting to police only the silent direction, on the grounds that v2→v1 rejects
itself. That is wrong twice over.

**TALM does not surface the rejection promptly.** `isManifestWorkReady`
(`controllers/utils/manifestwork.go:57`) returns `applied && available` with no way to
distinguish "failed to apply" from "not applied yet", and `IsManifestWorkCompleted`
(`:37`) returns `(false, nil)` in both cases. So a rejected kind looks exactly like a
slow upgrade until the `rolloutStrategy` timeout fires, and only then does
`handleManifestWorkTimeoutForCluster` (`controllers/manifestwork.go:89`) copy the
resource status in, with the real reason buried inside it. The "self-correcting"
direction is loud at the API server and silent in the IBGU.

This ambiguity is **inherent, not a bug to fix**: the work agent retries, so
`Applied=False` is a point-in-time state rather than a verdict, and even a missing CRD
stops being a failure the moment the CRD appears (see D10). TALM therefore cannot
reliably convert a mis-targeted apply into a prompt runtime failure at all — which is
precisely why the check has to happen before the rollout rather than during it.

**A mis-target that is invisible until runtime costs the whole plan item's budget.**
The engine already guards against a single stuck cluster monopolising the timeout:
`CalculateBatchTimeout` (`controllers/utils/batches.go:10`, called at
`clustergroupupgrade_controller.go:664`) gives each batch a fair share —
`remainingTime / remainingBatches`, where `remainingTime` subtracts the time already
spent — and its comment says so explicitly ("so that we don't let the entire timeout be
consumed by a single batch that gets stuck"). The last batch receives whatever is left.
A CGU-wide check at `:585` remains as a backstop. On batch timeout the default
`BatchTimeoutAction: Continue` records final state for that batch's clusters and
advances to the next; `Abort`, or a timeout on a canary batch, ends the CGU instead.

So one *isolated* bad cluster is contained: it wastes its batch's share and the rollout
continues with slightly less time for what remains. That is the case the design already
handles well.

The mis-targeting case is different, and worse, because the mismatch is not one unlucky
cluster — it is a **class** of spokes, so it is likely present in *every* batch. Every
batch then runs to its fair share and times out, the full budget is consumed, and the
CGU finishes `TimedOut` having upgraded nothing. Bounded, but total. A fleet-wide
mis-target is therefore not a slow rollout, it is a wasted maintenance window — and the
operator learns the reason only from manifest status buried under a generic timeout.
(Improving batch completion so one stuck cluster does not gate the rest of its batch is
a known backlog item, currently unprioritised. The hub-side design must not assume it is
fixed.)

Both of these argue the same way: a mismatch that is *detectable up front* must never be
allowed to reach the rollout, in either direction.

| Option | Pros | Cons |
|---|---|---|
| **4A** Fail closed — block the IBGU with a clear condition | Only option that prevents a silent no-backup upgrade; costs one validation pass instead of up to *b* × timeout | Blocks on detection gaps; needs a documented override |
| **4B** Fail open — warn and proceed | Never blocks a legitimate upgrade | Converts a detection gap into a silent data-loss event in the v1→v2 direction, and into a multi-batch stall in the v2→v1 direction. An asymmetric variant — open for v2-flavoured IBGUs, closed for v1 — buys less than it looks, because the v2→v1 rejection is not surfaced promptly either |
| **4C** Per-cluster skip — exclude mismatched clusters, proceed with the rest | Partial progress on mixed fleets | Contradicts the one-generation-per-IBGU model; complicates batching and status |

### D4 recommendation

**4A, fail closed, in both directions, as a pre-flight check over the resolved cluster
set before the first CGU is created.**

**This does not depend on D2.** Every per-plan-item selector only ever *narrows* the base
selector (see the D2 predicate table), so the base set is a superset of every set any CGU
in the plan will act on. Resolving the base selector for validation is therefore
sufficient and conservative, whichever way D2 lands. IBGU does need to resolve that base
set itself — it does not today — but that is a read, not a change of ownership, and the
RBAC is already present.

Under 2B the check is tidier still, because the set validated is byte-for-byte the set
handed to each CGU rather than a superset of it.

- Run it during the IBGU's validation phase, ahead of `ensureCGUForPlanItem`
  (`imagebasedgroupupgrade.go:270`).
- A cluster's LCA generation does not change mid-rollout, so with a frozen set this is
  genuinely a one-shot check. Re-validate only clusters newly added to the set — which
  under 2B means none, since the set only ever shrinks.
- Treat unknown generation as a mismatch. Fail closed.
- Surface it as an IBGU condition naming the offending clusters and the generation
  detected for each, so the operator can fix the selector or the fleet rather than guess.
- Provide an explicit opt-out annotation for lab use.
- Cost depends on D3. Under 3F it is a single List of ManagedClusters. Under the 3C
  fallback it is N ManagedClusterViews up front — a real but bounded one-time cost,
  cheap against even a single avoided batch stall, and the views can be reaped once the
  check passes.

---

## D5. Who delivers BackupSpec CRs to the spoke?

| Option | Pros | Cons |
|---|---|---|
| **5A** TALM copies from hub (today's ConfigMap model, retargeted) | Self-contained; IBGU controls ordering; matches "curated sets per upgrade path" | Hub needs a BackupSpec representation (see D6); more copy machinery to get right |
| **5B** ZTP/policy delivers (CNF-28676); IBGU only references by name | No hub CRD needed; separation of concerns; scoping question largely dissolves | **IBGU gains an ordering dependency it cannot express** — no way to wait for a policy before firing Prep, and a missing reference is currently a silent skip (`:444`) |
| **5C** Hybrid — copy if present on the hub, else assume policy delivered | Flexible | Ambiguous failure modes; "did it not get copied or not get policy-delivered?" is undiagnosable |

**Recommendation: 5A**, with a hard pre-Prep readiness check. The curated-sets-per-upgrade-path
requirement points at TALM-managed content; policy-delivered content isn't something an
IBGU author assembles. If 5B is chosen instead, a readiness gate before Prep becomes
mandatory, not optional. Rule out 5C.

---

## D6. BackupSpec scoping

Only relevant under 5A. CNF-26659 currently records "name + namespace" refs (namespaced),
while its own YAML example shows `metadata.name` with no namespace — the ticket is
internally inconsistent and this needs settling explicitly.

| Option | Pros | Cons |
|---|---|---|
| **6A** Cluster-scoped on spoke and hub | Matches IBU's own shape; no spoke namespace prerequisite (drops OADP from `docs/ibgu/README.md:3`); no Namespace manifest needed | Multiple curated sets collide in one flat namespace on the hub; forces name prefixing, and prefixes would have to be rewritten in lockstep with the IBU refs |
| **6B** Namespaced on spoke and hub (CNF-26659 as written) | Simple, one CRD, hub organisation for free | **Reproduces the fake-namespace hack exactly** — the ref doubles as hub lookup key and spoke target, so the hub needs a phantom namespace purely to make the lookup resolve |
| **6C** Cluster-scoped on spoke, namespaced hub representation | Best of both: clean spoke identity, hub organisation by upgrade path; the IBU ref is just a name, so hub source location becomes TALM's business and the fake namespace dies by construction | Scope is baked into a CRD, so this means **two CRDs**. Ownership question: LCA shipping a hub-only type pollutes the spoke operator; TALM shipping a namespaced BackupSpec under `lcm.openshift.io` is architecturally consistent (it mirrors IBGU↔IBU exactly) but is a real new CRD — types, deepcopy, RBAC, validation, docs |

**Recommendation: 6C, with TALM owning the hub-side namespaced CRD.** It is the only
option that both kills the fake-namespace workaround and supports curated sets per
upgrade path. It mirrors the IBGU↔IBU relationship the operator already embodies. If the
extra CRD is judged too expensive, **6A** is the next best — accept flat naming on the
hub rather than inherit 6B's phantom namespace.

---

## D7. Where does the hub read the content from?

The two existing copy paths disagree, and that disagreement is the root of the
fake-namespace workaround:

- `getSecretManifest` (`:408`) reads from `ibgu.GetNamespace()` and **rewrites**
  `.metadata.namespace` to the spoke target (`:428`). Hub location and spoke location
  are decoupled.
- `getConfigMapManifests` (`:437`) reads from `cmRef.Namespace` and embeds the object
  untouched. Hub location *is* the spoke location, which is why `openshift-adp` must
  exist on the hub.

| Option | Pros | Cons |
|---|---|---|
| **7A** From the ref's namespace (status quo) | No new field | Carries the phantom-namespace requirement into the new API |
| **7B** From the IBGU's namespace, rewrite or strip namespace on the way out | Follows the pattern that already works; no phantom namespace; keeps hub layout independent of spoke layout | Implicit convention users must learn |
| **7C** Explicit source field on the IBGU spec | Unambiguous | More API surface; another immutable field to reason about |

**Recommendation: 7B**, defaulting to the IBGU's namespace. Under 6C the spoke object is
cluster-scoped, so the transform is "strip the namespace" rather than "rewrite" it.

---

## D8. Copy semantics and lifecycle on the spoke

**Metadata stripping.** The current helper clears `ResourceVersion`, `ManagedFields`,
`CreationTimestamp`, `UID` (`:441-446`) — right for a ConfigMap. For a CR it should also
strip `ownerReferences` (dangling hub refs can get the object garbage-collected on the
spoke) and `generation`. Given CNF-26659 settles that BackupSpec has no status
subresource and no controller, stale `status` and `finalizers` are not concerns.

**Deletion.** Manifests currently carry `DeleteOption: Orphan` (`manifestworkreplicaset.go:232`),
so copied content survives the manifestwork.

| Option | Pros | Cons |
|---|---|---|
| **8A** Keep Orphan | Backup content outlives Prep, as today | Stale BackupSpecs accumulate with nothing reaping them; under 6A a later IBGU's set can collide by name with a leftover |
| **8B** Delete with the manifestwork | No accumulation, no cross-IBGU collisions | Requires confidence that nothing post-Prep needs to re-read the BackupSpec |

**Recommendation: 8B for BackupSpec specifically.** BackupSpec is upgrade *input*, not
the backup data itself, so it need not outlive the manifestwork. This is a deliberate
deviation from the blanket orphan policy and should be recorded as such, after confirming
LCA does not re-read BackupSpec after Prep.

---

## D9. Behaviour when a referenced BackupSpec cannot be resolved

Today a missing source logs a warning and `continue`s (`:444`), and Prep proceeds. This
is worse than it looks: the completion index is derived from the gathered count —
`1+len(manifests)` at `manifestworkreplicaset.go:120`, pointing at the IBU appended last
(`:210`) — so dropping a source *also shifts the expected index to match*, and the
manifestwork reports complete. A typo'd reference yields a green Prep with no backup, and
nothing logs above Info.

| Option | Pros | Cons |
|---|---|---|
| **9A** Hard-fail the IBGU before creating the CGU | Catches typos at the only point where they are cheap to fix | Needs clear per-reference error reporting |
| **9B** Fail the plan item at Prep | Closer to where it is used | Wastes a rollout cycle |
| **9C** Status quo (warn and skip) | — | Silent no-backup. Should not survive this work |

**Recommendation: 9A**, validated alongside the D4 generation check, with the unresolved
reference names in the IBGU condition. Overlaps CNF-28671 but on the hub side of the line.

---

## D10. Status-feedback robustness (independent of everything above)

1. **Act on `is<Stage>Completed == "False"`, which is a genuine terminal signal.**
   `IsManifestWorkCompleted` (`controllers/utils/manifestwork.go:37`) only asks whether
   the value equals `"True"`, so an explicit LCA-reported failure is treated exactly
   like "not finished yet" and the cluster burns the full `rolloutStrategy` timeout.

   LCA's setters make the three states distinguishable:
   `SetPrepStatusInProgress` creates **only** `PrepInProgress=True`, so `PrepCompleted`
   does not exist during a normal Prep; `SetPrepStatusFailed` creates
   `PrepCompleted=False`; `SetPrepStatusCompleted` sets it `True`
   (`lifecycle-agent/controllers/utils/conditions.go:435,445,461`; Upgrade and Rollback
   follow the same pattern, `:377,393,403`). So:

   | Feedback value | Meaning |
   |---|---|
   | absent | condition not yet created — **normal in-progress**, not a signal |
   | `"False"` | LCA explicitly reported the stage failed — **terminal** |
   | `"True"` | complete |

   Reacting to `"False"` lets a batch finish as soon as its last live cluster finishes,
   instead of running out its share of the timeout, and attributes the failure
   correctly. Confirm first that LCA never recovers from a failed stage without an Idle
   transition, which would make `"False"` non-terminal.

   **Feasibility: there is one clean seam, but this is an engine change, not a status
   tweak.** The CGU engine deliberately treats policy and manifestwork rollouts the
   same way, and progress is modelled as a *monotonically advancing index* with no
   channel for terminal failure. `getClusterProgress`
   (`clustergroupupgrade_controller.go:961`) is the single polymorphic point, returning
   `(currentIndex, isSoaking, err)` and dispatching to
   `getNextNonCompliantPolicyForCluster` or `getNextManifestWorkForCluster`.
   `updateClusterProgress` (`:900`) then interprets it: `index >= size` ⇒ `Completed`,
   otherwise stay `InProgress`. The only other exit is the CGU-wide timeout.

   Widening that one return to carry a terminal-failure signal is the clean change, and
   the policy/manifestwork asymmetry it creates is **semantically honest rather than a
   special case**: policy compliance genuinely has no terminal state, since a
   NonCompliant policy may become Compliant at any time, so the policy path has nothing
   to report and correctly always returns none.

   What makes it more than a one-liner is the knock-ons:

   1. A new per-cluster progress state. `ClusterRemediationProgress.State` is specified
      as NotStarted / InProgress / Completed (`types.go:221,230-232`); `Failed` has to
      be added. Additive to the CRD status, but it is API surface.
   2. A `Failed` branch in `updateClusterProgress` that nils the index, emits an event,
      cleans up ManagedClusterViews, and reports "done" so the batch is not blocked.
   3. Batch-completion semantics change: `isBatchComplete` must count `Failed` as
      terminal-done.
   4. Per-cluster final state is currently only written on the timeout path.
      `addClustersStatusOnTimeout` (`:775`) is what populates `Status.Clusters[]`, and
      the only constants are `ClusterRemediationComplete` / `ClusterRemediationTimedout`
      (`constants.go:28-29`). Early failure needs a `ClusterRemediationFailed` and a
      path that runs outside the timeout handler — reusing `timedout` would be a lie in
      user-visible status.
   5. **A product decision, not just plumbing:** if a batch finishes with some clusters
      `Failed`, is the CGU `Succeeded`, `TimedOut`, or something new, and does the plan
      continue to the next batch? Today neither outcome fits.
   6. IBGU consumes these states — `syncStatusWithCGUs`
      (`imagebasedgroupupgrade.go:205-226`) switches on exactly the two existing
      constants. A third needs a case, mapping to `ClusterState.FailedActions`, which
      already exists, so that part is natural.

   **Worth doing, but it is an optimisation rather than a safety fix.** Per-batch fair
   shares (D4) already bound the damage from a stuck cluster, and `BatchTimeoutAction:
   Continue` already keeps the rollout moving. What early detection adds is reclaiming
   each batch's unused share — material on a fleet-wide mis-target, where otherwise
   every batch runs out its full allowance — plus a correctly attributed failure instead
   of a generic timeout. That is a real benefit, but it does not prevent a class of
   failure, so it should be scoped as its own story with item 5 settled up front rather
   than carried as a bullet here.

   **Cheaper fallback if the engine change is rejected:** report only. Surface the
   terminal failure message in `Status.Clusters` and conditions as soon as it is seen,
   without touching the state machine. The operator learns the cause within a reconcile
   instead of at timeout. It does not stop the budget draining, so it buys the
   diagnostic half of the benefit for a small fraction of the work.

   **Rejected: treating an absent feedback value as a broken-rule signal.** Absence is
   the normal state for the whole duration of a stage, because the `*Completed`
   condition is not created until the stage ends. A malformed JSONPath (item 3) and a
   perfectly healthy in-progress Prep are indistinguishable by absence alone.

   **Also rejected: treating `ManifestApplied=False` as terminal.** The work agent
   retries continuously, so it is a point-in-time state, not a verdict — even "no
   matches for kind" resolves itself the moment the CRD appears. Using it to fail a
   cluster would manufacture false failures where today's timeout eventually succeeds.
   What it *can* support is diagnosis: surface the `Applied` condition reason and the
   per-manifest conditions in IBGU status **while waiting**, so an operator can see why
   a cluster is stuck without waiting out the timeout. That is a visibility improvement,
   not terminal detection.
2. **Source the IBU condition type names from LCA's API package** (CNF-28751). They exist
   and are exported in LCA's `controllers/utils/conditions.go`, but that is a
   controller-support package with no stability contract; relocating them to
   `api/imagebasedupgrade/v1` makes a rename a compile error in TALM at vendor-bump time.
3. **Fix the five malformed JSONPaths** (OCPBUGS-132380) — diagnostic-only, but they
   suppress exactly the message you need when 1 and 2 fail.

All three can land ahead of any D1 decision.

---

## D11. Release sequencing

- A v2-capable TALM must ship **no later than** the LCA carrying IBU v2, or the hub
  cannot drive upgraded spokes.
- A v2-capable TALM shipping **earlier** is harmless under 1A/1B/1D, since the old path
  is untouched.
- Under 1C it is not harmless — the storage-version choice starts pruning as soon as the
  CRD lands, independent of whether any v2 spoke exists. Another argument against 1C.

**Recommendation:** decouple the releases by choosing a D1 option that is safe to ship
early, and ship TALM first.

---

## Questions for LCA (CNF-26659)

1. **Will LCA keep *honouring* `oadpContent` as a deprecated input alongside BackupSpec
   (D1 option 1A(ii))?** This is the highest-value question in the list. CNF-26659
   currently settles the opposite ("LCA never reads the Velero format at runtime in
   v2"), but PR#7818 already keeps the Velero CR format, so this is a decision not to
   delete shipping code rather than to build anything. A yes removes the hub's
   coexistence requirement, justifies LCA staying on `v1`, and removes the forced
   migration behind most of CNF-28772's Wave 2 pain. If yes, pair it with a declared
   removal release so the deprecation completes.
2. If `oadpContent` is retained in the schema but **not** honoured (1A(i)): is a
   populated `oadpContent` rejected during Prep, or silently ignored? The field would
   still be in the schema, so the spoke can see it — this is a specifiable behaviour,
   and the answer decides whether 1A(i) has one silent mis-targeting direction or two.
3. BackupSpec scope: cluster-scoped or namespaced? Settled decision 2 says
   "name + namespace"; the YAML example omits the namespace. These disagree.
4. During the release where LCA still serves IBU v1 for conversion, what happens to a v1
   IBU created by the hub — is `oadpContent` readable, or pruned to v2 at rest? The
   ticket's own open item ("confirm the exact pruning behaviour of a None-strategy
   conversion on read") governs whether TALM can ever safely emit v1 to a v2 spoke.
5. Does LCA re-read BackupSpec after Prep (decides D8)?
6. Are the IBU status condition type names stable across the change? (Recorded as an
   assumption; CNF-28751 is the enforcement mechanism.)

---

## Appendix — verified code facts

| Fact | Location |
|---|---|
| `IBUSpec` embeds `lcav1.ImageBasedUpgradeSpec`, Required, CEL `self == oldSelf` | `pkg/api/imagebasedgroupupgrades/v1alpha1/types.go:33` |
| LCA pinned at `v0.0.0-20250227204303-42df68297836`; only `api/imagebasedupgrade/v1` vendored | `go.mod:27` |
| `createIBU` copies the spec verbatim into the spoke IBU | `controllers/imagebasedgroupupgrade.go:258,264` |
| OADP ConfigMaps gathered for Prep only | `controllers/imagebasedgroupupgrade.go:301` |
| ConfigMap copy reads from the ref's namespace, no rewrite; missing source warns and continues | `controllers/imagebasedgroupupgrade.go:437,444` |
| Secret copy reads from the IBGU namespace and rewrites the target namespace | `controllers/imagebasedgroupupgrade.go:408,428` |
| IBGU passes label selectors through to each generated CGU | `controllers/utils/manifestworkreplicaset.go:335` |
| Per-plan-item selector narrowing by `ibgu-<action>-completed` labels | `controllers/utils/manifestworkreplicaset.go:264` |
| IBGU writes action labels to ManagedClusters; partial failure possible | `controllers/imagebasedgroupupgrade.go:126` |
| All `ibgu-*-(completed\|failed)` labels wiped once a cluster completes Abort / AbortOnFailure / FinalizeUpgrade / FinalizeRollback | `controllers/imagebasedgroupupgrade.go:134-138` |
| Multi-IBGU workflow (separate IBGU for Abort / Rollback) is documented, intended behaviour | `docs/ibgu/README.md:193,195` |
| Completion index derived from gathered manifest count | `controllers/utils/manifestworkreplicaset.go:120,210` |
| Work-agent ClusterRole grants group+resource, version-agnostic | `controllers/utils/manifestworkreplicaset.go:144` |
| Copied manifests orphaned on delete | `controllers/utils/manifestworkreplicaset.go:232` |
| IBU apiVersion derived from a local scheme, not hardcoded | `controllers/utils/manifestworkreplicaset.go:241` |
| Seven functions typed on `*lcav1.ImageBasedUpgrade` | `controllers/utils/manifestworkreplicaset.go:20,41,62,83,104,170,241` |
| Five malformed feedback JSONPaths (OCPBUGS-132380) | `controllers/utils/manifestworkreplicaset.go:29,50,71,92,113` |
| Action resolved from the last dash-token of the MWRS name | `controllers/utils/manifestworkreplicaset.go:351` |
| CGU plan order sorted by the last dash-token of the CGU name, as a string | `controllers/utils/common.go:145` |
| CGU ownership label is `ibgu: <name>`, no kind discriminator | `controllers/utils/constants.go:185` |
| `getFieldValue` returns `""` for both absent and non-matching feedback | `controllers/utils/manifestwork.go:79` |
| Each batch gets a fair share of the remaining budget (`remainingTime / remainingBatches`); last batch takes the rest | `controllers/utils/batches.go:10`, called at `clustergroupupgrade_controller.go:664` |
| CGU-wide timeout remains as a backstop | `controllers/clustergroupupgrade_controller.go:585` |
| On batch timeout, default `BatchTimeoutAction: Continue` advances to the next batch; `Abort` or a canary batch ends the CGU | `controllers/clustergroupupgrade_controller.go:673-726`, `pkg/api/.../types.go:96-103,191` |
| `addClustersStatusOnTimeout` records final state only for the batch that timed out | `controllers/clustergroupupgrade_controller.go:775,794` |
| Progress is a monotonic index with no terminal-failure channel; single dispatch point for both rollout types | `controllers/clustergroupupgrade_controller.go:961,900` |
| Per-cluster progress states are NotStarted / InProgress / Completed only | `pkg/api/clustergroupupgrades/v1alpha1/types.go:221,230-232` |
| Only two cluster final-state constants exist | `controllers/utils/constants.go:28-29` |
| `isManifestWorkReady` cannot distinguish "failed to apply" from "not yet applied" | `controllers/utils/manifestwork.go:57` |
| Per-cluster ManifestWork created by verbatim template copy | `controllers/utils/manifestwork.go:134` |
| CGU `Enable *bool`; gate sits after cluster selection but before the remediation plan is built | `pkg/api/clustergroupupgrades/v1alpha1/types.go:141`, `controllers/clustergroupupgrade_controller.go:383,1120` |
| `validateCR` hard-fails when a listed cluster is not a ManagedCluster | `controllers/clustergroupupgrade_controller.go:1345-1347` |
| `maxConcurrency` auto-clamps to the cluster count | `controllers/clustergroupupgrade_controller.go:1367-1372` |
| IBGU already holds `managedclusters` get/list/watch/patch/update | `config/rbac/role.yaml:63-69` |
| TALM reads no spoke version or capability data today (no `openshiftVersion`, ClusterClaims or ManagedClusterInfo) | absent across `controllers/`, `pkg/` |
| `ManagedCluster.status.clusterClaims` (`[]{Name,Value}`) already vendored and available to TALM | `vendor/open-cluster-management.io/api/cluster/v1/types.go:168,188` |
| LCA publishes no ClusterClaims today | `openshift-kni/lifecycle-agent` (no matches) |
| OCM has no claim-to-label sync; the spoke agent writes claims only to `status.clusterClaims` | `ocm/pkg/registration/spoke/managedcluster/claim_reconcile.go:124` |
| Custom claims capped at 20 by default, sorted by name and truncated; event is recorded on the spoke | `ocm/pkg/registration/spoke/options.go:54`, `claim_reconcile.go:115-124` |
| Claims matching a reserved name or suffix are exempt from the cap | `ocm/pkg/registration/spoke/managedcluster/claim_reconcile.go:95-101` |
| `Placement` supports `claimSelector` beside `labelSelector` | `vendor/open-cluster-management.io/api/cluster/v1beta1/types_placement.go:146,232` |
| TALM uses Placement only as an output of selection (pins explicit names); no `placementdecisions` RBAC | `controllers/utils/policy_util.go:269`, `config/rbac/role.yaml` |
| `lcav1` is not registered in the manager scheme — IBUs are only ever serialised | `main.go:68-79` |
| No conversion-webhook infrastructure; CSV has no `webhookdefinitions` | `config/crd/kustomization.yaml:13`, `bundle/manifests/*.clusterserviceversion.yaml` |
| Spoke prerequisite currently documents both LCA and OADP | `docs/ibgu/README.md:3` |
| LCA condition types exported, but from a controller-support package | `lifecycle-agent/controllers/utils/conditions.go` |
| `<Stage>Completed` condition is not created until the stage ends; in-progress sets only `<Stage>InProgress=True` | `lifecycle-agent/controllers/utils/conditions.go:435,461` (Prep), `:393,403` (Upgrade) |
| Stage failure sets `<Stage>Completed=False` — a distinguishable terminal value | `lifecycle-agent/controllers/utils/conditions.go:445` (Prep), `:377` (Upgrade) |
