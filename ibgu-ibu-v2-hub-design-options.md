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
     +- D6  BackupSpec shape: hub library, spoke scope     [moot if policy delivers]
         +- D7  Where the hub reads the content from
             +- D8  Copy semantics and lifecycle on the spoke
                 +- D9  Behaviour when a reference is unresolvable

D10 Status-feedback robustness       <- independent, can land immediately
D11 Release sequencing               <- depends on D1
```

D1 and D5 are the two that actually need a decision meeting. D2 is independently
worth doing. D6 is mostly a question *for* LCA now — an exported content type, name-only
references and a fixed read namespace settle it — and the rest follow. Those same two asks
also decide whether the IBGU needs a new reference field at all: with name-only refs and a
fixed read namespace, a per-entry reference list (6C) can reuse LCA's own field inside
`ibuSpec`, and only a set reference (6D) needs a sibling.

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
| **3F** LCA publishes its capabilities as a ClusterClaim | The idiomatic ACM mechanism for a spoke to assert a fact to the hub. Surfaces as `ManagedCluster.status.clusterClaims`, so the hub reads the whole fleet in **one List** — no per-cluster objects, no reconcile wait. Type is already vendored (`vendor/open-cluster-management.io/api/cluster/v1/types.go:168,188`) and TALM already holds `managedclusters` get/list/watch, so the hub side costs nothing new. Removes the version→capability table entirely and serves every future contract change | Requires an LCA change, so not unilateral. **Cannot be retrofitted into already-shipped LCA versions**, so "claim absent" stays ambiguous between a pre-feature LCA, a truncated claim (see the cap note below) and an LCA-side failure. A broken sync path shows as *staleness* rather than absence, and is caught by the availability gate below, not by the claim. Becomes a public contract LCA must then maintain |

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

### A missing claim is not a sign that ACM is broken — check that directly, first

The three spoke-to-hub paths in play run through different agents, so the failure of one
says little about the others:

| Mechanism | Spoke component | Surfaces on the hub as |
|---|---|---|
| ClusterClaim (3F) | OCM registration agent (klusterlet) | `ManagedCluster.status.clusterClaims` |
| ManifestWork via MWRS — the IBGU's only delivery path today | OCM work agent (klusterlet) | ManifestWork status feedback |
| ManagedClusterView / Action (3C) | ACM work-manager addon — not core OCM; the types are vendored from `stolostron/cluster-lifecycle-api` (`clustergroupupgrade_controller.go:49`) | MCV status |

Read against that, the causes of an absent claim are:

1. **LCA predates the claim** — the expected case, and the reason the fallback exists.
   ACM is healthy; MCV and ManifestWork both work.
2. **Truncated by the custom-claim cap** — the registration agent is working as designed;
   MCV is unaffected.
3. **LCA down, or its claim controller failing** — nothing in ACM is broken, and MCV can
   still read the LCA CSV because it does not go through LCA.
4. **Spoke unreachable or its agents broken** — the only case where MCV and ManifestWork
   are also dead. It should present as *stale* claims rather than absent ones, since the
   hub is not expected to clear `status.clusterClaims` when the lease expires (to confirm
   against OCM's hub registration controller; only the API is vendored here). The
   hub-visible signal is `ManagedClusterConditionAvailable` going Unknown or False.

So the pre-flight should not infer ACM health from the claim. It should check it, and in
this order, per cluster:

1. **Available.** `ManagedClusterConditionAvailable` not `True` → fail closed with
   "cluster unavailable"; do not consult the claim or the fallback. TALM already applies
   the same gate to the precache path, for the same reason — a View can be stale when the
   cluster stops updating it (`managedClusterResources.go:544-546`). The IBGU controller
   has no such check today.
2. **Claim.** Present and recognised → use it (D4 decides what follows).
3. **Fallback (3C).** Claim absent → read the LCA CSV by MCV. If the CSV shows an LCA
   version older than the one that first ships the claim, map it as 3C describes. If it
   shows a version that *should* publish the claim, the claim was truncated or LCA's claim
   controller is failing → fail closed with that specific reason.
4. **Fallback unavailable.** MCV does not resolve → "unknown", fail closed (D4), reporting
   the fallback path rather than the LCA as the cause.

Two consequences worth recording:

- **3C adds a dependency the IBGU does not have today.** Every IBGU action goes through
  MWRS→ManifestWork, which needs only the OCM work agent. The fallback additionally needs
  the work-manager addon. Distinguishing step 4 from a genuine LCA problem means reading
  that addon's `ManagedClusterAddOn` status — a read TALM does not do today, and likely
  new RBAC.
- **The fallback makes truncation diagnosable.** Without 3C, a truncated claim and a
  pre-feature LCA look identical, and the cap problem above is a blocked upgrade whose
  cause is invisible from the hub. With the step-3 version comparison, it becomes a named
  condition. That is an argument for keeping 3C even after the claim has shipped, at least
  until the claim is confirmed exempt from the cap.

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
  Where the generation is unknown, name *why*, using the reasons from the D3 check order:
  cluster unavailable, claim missing on an LCA that should publish it, or fallback (MCV)
  unavailable. Each points at a different owner — ACM, LCA, the work-manager addon — and
  a bare "unknown" points at none.
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

## D6. BackupSpec scoping — reframed as two objects rather than one scope

The *hub library object* below is only relevant under 5A. The spoke-side scope and the
shape of the reference matter under either delivery model — which is the argument for
keeping them independent of D5, made under 6C below.

CNF-26659 designed `BackupSpec` for the only consumer it had at the time: LCA, on the
spoke, reading it at Prep. The hub requirement is different in kind — it is a *library*
of curated backup content, organised per upgrade path, outliving any single IBGU. The
scoping question only exists if the same object is made to play both roles. It does not
have to be, and splitting them is cheaper than it looks.

| Role | Object | Scope | Owner | Lifetime |
|---|---|---|---|---|
| Library (hub) | TALM kind in `lcm.openshift.io` — a `BackupContent` entry, or a `BackupSet` of entries (6D) | **Namespaced** — one namespace per upgrade path | TALM | Long; curated, reused by many IBGUs |
| Upgrade input (spoke) | `BackupSpec` in `lca.openshift.io` | **Namespaced, in a namespace LCA owns** (6B′), or cluster-scoped under 6A | LCA | Short; materialised by TALM per IBGU |

The mapping between the two is entirely TALM's business: the hub entry is what an author
curates, the spoke `BackupSpec` is what LCA reads, and TALM produces the second from the
first at Prep. What that mapping need not be is a rename — under 6C the spoke name *is* the
entry name (D8), the derivation being the identity, so the two objects carry the same name
in different namespaces. That coincidence is deliberate, and it is why the IBGU can use
LCA's own reference field instead of a new one. 6B only went wrong because the ref carried
a *namespace* that had to resolve on both sides; with that gone, the name may legitimately
mean both things.

Give the hub kind a distinct name. Two kinds called `BackupSpec` in two APIs is a
standing source of confusion in docs and in `oc get`, and the hub object is not a
specification of anything — it is content.

### Consequence for the IBGU CRD: reuse LCA's field when the two names coincide

Whether a new field is needed at all follows from the ref shape, and the ref shape is
D6's remaining choice:

- **6C with name-only refs (recommended).** The hub entry `dpa-config` in the IBGU's
  namespace becomes the spoke object `dpa-config` in the namespace LCA owns. The list the
  author writes is exactly the list the IBU needs, so it goes in
  `spec.ibuSpec.backupRefs` — LCA's own field, which is already part of the IBGU schema
  because the spec is embedded (`types.go:33`). No sibling, no `!has()` rule forbidding
  one, no second source of truth.
- **6D with set refs.** The IBGU input is one set reference; the IBU needs N item names
  TALM synthesises (`<set>-<index>`). Different strings in different counts, so the input
  cannot live in the output field. The sibling (`backupSetRefs`) is required, together
  with `!has(self.ibuSpec.backupRefs)`.
- **6C with a namespaced ref, or any hub-only refinement on the ref.** Same as 6D: once
  the ref carries something the spoke ref does not, the lists differ and the sibling is
  required.

Neither shape is foreign to this CRD. `clusterLabelSelectors` and `plan` are already
TALM-owned siblings of `ibuSpec` (`types.go:34-45`), and `createIBU` has always stamped
`Spec: ibgu.Spec.IBUSpec` verbatim (`controllers/imagebasedgroupupgrade.go:258-264`).
Reuse keeps that a pure struct copy; the sibling makes it "copy, then rewrite the backup
refs", which is a small but real loss of the "the IBU spec is LCA's spec" property.

**Why reuse is preferred where it applies:**

- **One authoring surface across 5A and 5B.** Delivery is still open. With reuse the IBGU
  looks the same whichever wins — only whether TALM copies the content differs. With a
  sibling, switching to policy delivery changes the IBGU schema, which is a user-facing
  change riding on a decision the spike has not made.
- **Nothing to keep in sync.** No CEL rule forbidding the embedded field, no validation
  that two lists agree, no "which one is authoritative" in the docs and samples.
- **Immutability for free.** `ibuSpec` is already `self == oldSelf`, so refs cannot be
  re-pointed mid-flight without a new rule. A sibling would need its own.
- **The IBU spec on the IBGU is the IBU spec.** What the author writes is what the spoke
  receives; the hub-specific work is copying the referenced content, not rewriting the
  spec.

**What reuse costs:**

- **The IBGU schema tracks LCA's field.** This is the coupling the sibling was added to
  narrow. It is bounded — the shape is a list of names — but real, and it is the main
  reason to insist on name-only refs (ask 2).
- **It forecloses 6D, or makes a later move to 6D an API break.** Spoke names equal hub
  names only while D8 holds; if concurrent overlap is ever supported and names get
  qualified, the two lists diverge and the field has to split. A sibling keeps that door
  open at the price of one field.

**What reuse does not change:** TALM still resolves and validates the refs (D9), and the
spoke object still has to be in place when LCA runs Prep — shipped by TALM in the Prep
manifestwork under 5A, delivered by policy beforehand under 5B. Reuse moves where the refs
are written, not who checks them.

### Three questions the reference design creates

1. **Hub→spoke naming.** The spoke target is one flat name space — the cluster under 6A,
   LCA's namespace under 6B′ — so two referenced library entries in different namespaces
   that share a name cannot both be injected under their own names. Two ways out, both
   raised on review:

   - **Take 7B literally: refs resolve in the IBGU's own namespace, no override.** Names
     are then unique by construction and nothing needs validating. Cost: an entry cannot
     be shared across namespaces, which is acceptable if curation is per upgrade path —
     each path has its own namespace and its own entries. **This is the option reuse
     requires**, because uniqueness per namespace is what lets one name mean both things.
   - **Make the hub object a *set* rather than an entry (option 6D, below).** No
     per-item names exist anywhere, so the collision cannot be expressed, and TALM owns
     the spoke naming outright — at the cost of the sibling field and the separate
     authoring surface.

   The alternative previously proposed here — identical names plus a uniqueness check
   across the referenced set — is now unnecessary: 7B-restricted handles it with less
   machinery, and 6D removes the question entirely.
2. **Content pinning.** `ensureManifests` runs on every reconcile (`:100`) and
   `createIBU` is called per plan item (`:292`), so copied content tracks the hub object
   continuously. Editing a library entry mid-IBGU means later batches get different backup
   input than earlier ones. Today's ConfigMap path has exactly the same property. Make the
   library entry's spec immutable and treat changes as "new entry, new IBGU" — consistent
   with how `ibuSpec` already works.
3. **One name, two objects.** Under reuse the same string is a hub entry name and a spoke
   object name. That is deliberate rather than sloppy — one rule, stated once (D8 and 6B′),
   relied on twice — but the docs have to say it plainly: an author reading
   `backupRefs: [{name: dpa-config}]` should understand that TALM will *make* `dpa-config`
   exist on the spoke, not that it already does.

### Two asks on CNF-26659 that make this clean rather than merely possible

Both are small, and both are worth making now while the API is still in flight:

1. **Factor the content into an exported spec type.** If LCA exposes the payload as a
   named Go type (say `BackupSpecSpec`) rather than only as fields of the CR struct,
   TALM's hub type embeds *that* — `spec.template: lcav1.BackupSpecSpec`. One content
   schema, owned by LCA, versioned by LCA. Without it the hub type either duplicates the
   fields (two schemas to keep in sync, which is the compile-time-vs-copy coupling
   CNF-28751 exists to kill) or embeds a whole `BackupSpec` object as an opaque template.
   Both fallbacks work; they are just less honest about the ownership boundary.
2. **Name-only references, and a fixed read namespace.** The IBU's reference list should
   not carry a namespace: a name or `LocalObjectReference`. The namespace LCA reads from
   should be a constant it owns and exports from its API package (6B′), not a value the
   caller supplies. Together these settle the ticket's internal inconsistency — the
   "name + namespace" decision against the example that omits the namespace — rather than
   filing it, and they are what make a namespaced spoke object safe. They are also what
   let the IBGU reuse LCA's own reference field (above): name-only plus a fixed namespace
   is exactly the condition under which the hub and spoke names coincide.

### What the reframe collapses

- **D7** becomes structural rather than conventional. The hub object is namespaced, and
  under 6C the ref is confined to the IBGU's namespace. The transform on the way out is
  "strip metadata" (plus "set the fixed namespace" under 6B′) — the name is already the
  one to use, so there is no implicit convention for a user to learn.
- **D9**'s hard-fail becomes almost free: the ref must resolve to a hub object in a known
  namespace, so "unresolved" has exactly one meaning.
- **D5** is unaffected in its reasoning but clearer in its price: the hub library object
  *is* the CRD that 5B exists to avoid. 5B's ordering con is unchanged, and if the CRD is
  accepted for D6 then 5A costs nothing further.

There is precedent in the tree: `PreCachingConfig` is a TALM-owned, namespaced, hub-side
content CR, referenced from the CGU by `NamespacedCR`
(`pkg/api/clustergroupupgrades/v1alpha1/types.go:46-47`; CRD scope `Namespaced` in
`config/crd/bases/ran.openshift.io_precachingconfigs.yaml`). Reusing that pattern is a
known-shape cost, not a new one.

### Cost, stated plainly

TALM still ships a CRD: types, deepcopy, RBAC, validation, docs, samples, kuttl. The
reframe makes it *thin* — hub metadata plus one embedded content struct — it does not
make it free. The type's shape also depends on the D1 outcome: under 1A the embedded
content struct serves both generations; under 1D the payload needs a generation tag.
Keeping the content in a versioned subfield leaves both paths open.

### Option 6B′ — namespaced on the spoke, read from a namespace LCA owns

Raised on review, and it rehabilitates 6B by removing the one thing that made it wrong.
The spoke object stays **namespaced**, but the namespace is not carried in the ref and not
chosen by the hub: LCA reads its input from a fixed namespace it owns
(`openshift-lifecycle-agent`, or a dedicated one — see the ownership note below). The IBU
ref is then a bare name, and where the content lives is LCA's business.

6B as written conflated three separate problems. Only the first one was fatal, and this
option removes it:

1. **The ref doubled as hub lookup key and spoke target**, so the hub had to host a
   phantom `openshift-adp` namespace. Under a fixed read path the ref is a spoke-side name
   TALM chose; there is nothing to resolve on the hub through it.
2. **Spoke scope.** Namespaced versus cluster-scoped is a privilege and identity question,
   not a correctness one. `BackupSpec` is data; `IBU` is an operation that drives a
   node-level reboot, which is what justifies cluster scope for the IBU and not for this.
3. **Content namespace on the hub mirroring the spoke.** That is 7B's job, and 7B already
   decouples them.

```yaml
# hub, in the IBGU's namespace (7B) — shape per 6C or 6D
apiVersion: lcm.openshift.io/v1alpha1
kind: BackupContent
metadata:
  name: dpa-config
  namespace: upgrade-paths
spec:
  template: <lcav1.BackupSpecSpec>
```

```yaml
# spoke, IBU constructed by TALM
apiVersion: lca.openshift.io/v1
kind: ImageBasedUpgrade
metadata:
  name: upgrade
spec:
  backupRefs:                        # name-only, no namespace anywhere
    - name: dpa-config
```

```yaml
# spoke, copied by TALM into the namespace LCA owns
apiVersion: lca.openshift.io/v1
kind: BackupSpec
metadata:
  name: dpa-config
  namespace: openshift-lifecycle-agent   # constant, owned by LCA
spec: <lcav1.BackupSpecSpec>
```

**Where the refs live: in LCA's own field, reused.** Because the spoke name is the entry
name, the list the author writes is the list the IBU needs. It goes in
`spec.ibuSpec.backupRefs` — LCA's field, already present in the IBGU schema because the
spec is embedded (`types.go:33`) — and the IBGU carries no second reference field:

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: ImageBasedGroupUpgrade
metadata:
  name: upgrade-4-22
  namespace: upgrade-paths
spec:
  ibuSpec:
    seedImageRef:
      version: 4.22.3
      image: quay.io/example/seed:4.22.3
      pullSecretRef:
        name: pull-secret
    backupRefs:                      # LCA's field: the hub entry name, and the spoke name
      - name: dpa-config
  clusterLabelSelectors:
    - matchLabels:
        upgrade-path: "4-22"
  plan:
    - actions: ["Prep"]
      rolloutStrategy: {maxConcurrency: 1, timeout: 240}
    - actions: ["Upgrade"]
      rolloutStrategy: {maxConcurrency: 1, timeout: 240}
```

The IBU that `createIBU` ships carries the same list unchanged — `Spec: ibgu.Spec.IBUSpec`
stays a pure struct copy (`:258-264`) — and what TALM adds is not a rewrite of the spec but
the `BackupSpec` object behind the name, copied into LCA's namespace as the Prep manifest:

```yaml
spec:
  stage: Prep
  seedImageRef: { version: 4.22.3, image: quay.io/example/seed:4.22.3, pullSecretRef: { name: pull-secret } }
  backupRefs:
    - name: dpa-config   # unchanged from the IBGU; LCA reads it from its own namespace
```

Four requirements this one-field form places, none of which are documentation items:

- **Name equality is a rule, not a coincidence.** D8 fixes the spoke name to the entry
  name and 6B′ fixes the namespace on both sides; that is what makes one string valid in
  both places. If either changes — qualified names under concurrent overlap — the field
  must split, as the sibling variant would have done from the start.
- **A ref that resolves on the hub is a promise on the spoke.** TALM resolves every ref on
  the hub before creating the CGU, fails closed if one is missing (D9), and ships the object
  in the Prep manifestwork ahead of the IBU (`manifestworkreplicaset.go:211-213`). Today a missing source
  is a silent skip (`:444`).
- **This is available under 6C only.** Under 6D the input is a set ref and the output is
  `<set>-<index>`; the sibling (`backupSetRefs`) and its `!has()` rule return.
- **The IBGU looks the same under 5A and 5B.** Under policy delivery the author writes the
  same field and TALM copies nothing; the difference is invisible in the schema, which is
  the point.

The IBU field is D6 ask 2 in its most natural form: a name-only list, because the namespace
is no longer the caller's to supply. The constant itself should be exported from LCA's API
package and documented as a commitment, for the same reason CNF-28751 exports the condition
type names — a configurable namespace would mean the hub has to be told it, which puts the
lookup key straight back.

**What it buys:**

- **No cluster-scoped content object on the spoke.** Least privilege for a data object, and
  no new cluster-scoped CRD in LCA.
- **The `oadpContent` migration becomes a kind-and-namespace change, not a scope change.**
  Today's content is namespaced ConfigMaps; this keeps that property.
- **One read path for LCA.** It reads one namespace it controls, rather than whichever
  namespace a ref names — a fleet-scoped read grant over caller-chosen namespaces would be
  the worse privilege outcome.
- **The hub has no target-namespace decision to make or validate**; the target is a
  constant.
- **Reaping (8C) gets materially cheaper.** Every copy for every IBGU on a spoke lands in
  one namespace, so the sweep is a single List filtered against the names the IBGU
  recorded, instead of a cluster-wide scan over cluster-scoped objects.

**What it costs, and what has to be pinned down:**

- **Namespace ownership.** LCA must create and own the namespace; TALM must not also create
  it in the manifestwork, or two owners race. Reusing LCA's operator namespace costs
  nothing and needs no new object, but mixes user content with LCA's own configuration and
  inherits the operator namespace's RBAC. A dedicated LCA-owned namespace (say
  `openshift-lifecycle-agent-backup`) is cleaner at the price of one Namespace plus one
  RBAC binding.
- **Content dies with the namespace.** A cluster-scoped object survives namespace deletion;
  these do not. A cluster-admin cleaning up the LCA namespace silently removes pending
  backup input, and the failure surfaces only at pre-pivot. That needs to be documented on
  the LCA side, and it is the one axis where 6A is better.
- **The collision surface contracts to one namespace on the spoke.** Every IBGU targeting
  that spoke writes there. D8 settles this as the bare entry name, on the argument that a
  path change always goes through Prep and Prep overwrites before anything reads. If that
  assumption is ever relaxed — concurrent IBGUs on one cluster — this is where the cost
  lands, and it is the point at which namespace qualification would come back.
- **LCA must act only on the names the IBU references**, not on every `BackupSpec` in the
  namespace, or an unrelated object changes LCA's behaviour.
- **It does not give the hub a library.** 6B′ settles the spoke side only; the hub still
  needs a TALM-owned object (6C or 6D).

**The variant this opens, and why it is declined.** If the spoke object is a namespaced LCA
`BackupSpec`, the hub could use LCA's *own* CRD and copy objects verbatim — one schema, no
TALM CRD, and ask 1 disappears. Declined: one CRD means one schema on the hub for a mixed
fleet, so the hub's `BackupSpec` shape would have to represent every LCA generation at
once. That is precisely the problem D1 exists to manage, relocated onto the content axis,
and it also means TALM ships another component's CRD. The two-object split keeps the hub
schema TALM's to version.

**Recommendation: adopt 6B′ over 6A regardless of the 6C-versus-6D choice.** It is better
on privilege, on the migration story and on reaping, and worse only on content surviving
namespace deletion. The table at the top of D6 reflects this: the namespaced reading is
6B′, and cluster scope is 6A. Whatever LCA decides, the ref itself stays name-only.

### Option 6C — the hub object is an *entry*

One hub CR per backup definition: a namespaced, TALM-owned kind whose payload is LCA's
content schema and whose name the user chooses.

```yaml
apiVersion: lcm.openshift.io/v1alpha1
kind: BackupContent                 # TALM-owned, namespaced
metadata:
  name: dpa-config                  # user-chosen, unique within the namespace
  namespace: upgrade-paths          # one namespace per upgrade path
spec:
  template:                         # payload only — lcav1.BackupSpecSpec
    <lcav1.BackupSpecSpec>
```

The IBGU references entries by name, in LCA's own field, and the reference is confined to
the IBGU's own namespace (7B-restricted):

```yaml
spec:
  ibuSpec:
    seedImageRef: ...
    backupRefs:                     # LCA's field, reused: the hub entry name (see above)
      - name: dpa-config            # namespace omitted: the ref cannot point elsewhere
  plan: ...
```

There is deliberately no `namespace` on the ref. That is what restricting to 7B buys:
uniqueness becomes a per-namespace property, so a bare name identifies an entry with no
separate uniqueness contract to validate — and it is what lets the same name also be the
spoke object's name. If a `NamespacedCR` ref shape were preferred for consistency with
`PreCachingConfig`, the namespace would have to be pinned to the IBGU's own by CEL, and the
field could no longer be reused: the two sides would no longer name the same string.

The spoke target is one flat name space — the cluster under 6A, the LCA-owned namespace
under 6B′ — so the copied object is named after the hub entry itself (`dpa-config`, not
`upgrade-paths-dpa-config`). D8 explains why that is safe rather than sloppy: a new upgrade
path necessarily re-Preps, and Prep overwrites the same names before anything reads them.
6D derives a name the same way, with the set and index added; that follows below.

### Option 6D — the hub object is a *set*, not an entry

Proposed on review, and the more literal reading of the stated requirement. Instead of one
hub CR per backup definition, the hub CR is a namespaced, TALM-owned **set** carrying an
inline list of LCA content specs, with no per-item names:

```yaml
kind: BackupSet                     # lcm.openshift.io/v1alpha1, namespaced
spec:
  backupSpecs:
    - <lcav1.BackupSpecSpec>        # unnamed, ordered
    - <lcav1.BackupSpecSpec>
```

The IBGU then carries set references, not item references:

```yaml
spec:
  ibuSpec:
    seedImageRef: ...
  backupSetRefs:
    - name: path-4-22
      namespace: upgrade-paths
  plan: ...
```

TALM derives the spoke object names and constructs the IBU's ref list from them.
Uniqueness becomes TALM's internal invariant over a list it is building, not a contract
users have to satisfy — which is the substantive win. D8 fixes the shape of the derived
name: `<set>-<index>`, **not** `<ibgu>-<index>`. Only the `Prep` item copies content and
the `Upgrade` item consumes it, so a name keyed on the IBGU breaks as soon as a different
IBGU re-applies the IBU; a name keyed on the set does not. The hub namespace is
deliberately not in it either — a new upgrade path must run Prep, and Prep overwrites the
same names before anything reads them (D8, constraint 2).

| | 6C + entries + 7B restricted | 6D set + derived names |
|---|---|---|
| Name collisions | impossible because refs are confined to the IBGU's namespace | impossible within a set; across sets it relies on the same non-overlap assumption as 6C (D8) |
| Cross-namespace reuse | not possible | possible — the set ref names its namespace |
| Matches the stated requirement | partly: "curated sets per upgrade path" is expressed as N loosely-related objects | directly: the set *is* the curation unit, and the IBGU carries one ref per set |
| User-facing name management | every entry needs a hub name and the IBGU repeats it | none |
| Atomic curation | per entry | per set — a set can be validated as a whole (ordering, mutually exclusive backup types) |
| Item-level reuse across sets | yes | no; a shared item is duplicated |
| Failure granularity | per reference | the whole set fails closed, which is correct but coarser |
| Dependency on D6 ask 1 | nice to have | effectively required, or the list holds whole objects and meaningless `metadata` inside a spec |
| IBGU authoring field | LCA's own `backupRefs`, reused | a new sibling plus a `!has()` rule |

Composability survives: an IBGU can reference several sets, and derived names
`<set>-<index>` keep them distinct within a path. Diagnostics survive too, since derived
names appear in the IBU refs, the manifestwork and the IBGU status.

One refinement worth keeping despite "no names": an **optional** `name` per item,
validated for uniqueness *within its own set* only. That restores readable spoke names for
the failure case ("backup `dpa-config` failed") without reintroducing a cross-object
uniqueness contract, because a set's items never share a spoke with another set's.

**6D's price is the reference field.** A set ref and its derived item names are different
strings in different counts, so the IBU's ref list cannot be the IBGU's input: 6D needs a
TALM-owned sibling (`backupSetRefs`), a CEL rule forbidding `ibuSpec.backupRefs`, and a
`createIBU` that rewrites the spec instead of copying it. It also makes the IBGU's shape
depend on D5 — under policy delivery there is no set to compute names from. 6C with
name-only refs avoids all three, because the spoke name is the entry name.

**Recommendation: 6C + 7B-restricted + reuse LCA's field**, now that reuse is on the table.
The earlier lean toward 6D was on eliminating the naming question, and D8 has since
eliminated it for 6C as well: the spoke name is the entry name, the namespace is 7B's,
and no uniqueness contract remains beyond the one the namespace already provides. 6D's
remaining advantages are atomic per-set validation and one ref instead of N — real, but
not worth a second reference field plus a delivery-dependent schema. **Choose 6D instead
only if item ordering and mutual exclusivity genuinely need to be validated as a unit at
the hub**, in which case the sibling field and the CEL rule come back with it.

### The copied spoke BackupSpec, and how TALM names it

The hub object's shape differs between 6C and 6D, and the spoke scope between 6A and 6B′;
what does not differ is the served object's content. It is LCA's `BackupSpec`, carrying the
hub payload and nothing else:

```yaml
apiVersion: lca.openshift.io/v1          # same group as IBU; version per CNF-26659
kind: BackupSpec
metadata:
  name: dpa-config                       # chosen by TALM; see Naming below
  namespace: openshift-lifecycle-agent   # 6B′ only; absent under 6A
spec:
  <lcav1.BackupSpecSpec>                 # copied verbatim from the hub entry/set item
```

**Naming.** The spoke name has to satisfy four things at once, and only one convention
does:

| | Spoke name | Speaks into |
|---|---|---|
| 6C (entries) | `<entry-name>` — not derived at all | cluster (6A) or LCA-owned namespace (6B′) |
| 6D (sets) | `<set-name>-<index>` | same |
| 6D with the optional per-item name | `<set-name>-<item-name>` | same |

- **No IBGU name in it** (D8 constraint 1). Only `Prep` copies; `Upgrade` consumes. A name
  keyed on the IBGU breaks as soon as a different IBGU re-applies the IBU.
- **No hub namespace in it either** (D8 constraint 2). A new upgrade path must run Prep,
  and Prep overwrites these names before anything reads them, so two paths cannot both have
  a live copy at once. In 6C this is also what keeps the name equal to the hub entry's,
  which is what lets the IBGU reuse LCA's ref field.
- **It is stable**, so a re-copy under `Orphan` overwrites the same object instead of
  accumulating a second one — which is also what makes a replacement IBGU after a
  mid-flight deletion find the content the first one copied.
- **It must be a valid name.** Entry and set names are already constrained by their own
  CRDs; 6D adds at most one separator and one index, so validate the result on the IBGU at
  admission rather than truncating. 6C needs no extra check.

**Metadata.** The copy carries the payload only. Strip `resourceVersion`, `managedFields`,
`creationTimestamp`, `uid`, plus `ownerReferences` and `generation` as D8 requires; the
hub object's `namespace` and labels are not carried across. The name is the one exception
under 6C, and it is not "carried across" so much as *defined*: the spoke object's name is
specified to be the entry's name, which is why the same string can also sit in the IBU's
ref list.

**Why a predictable name is worth having on its own.** It appears in the IBU's backup-ref
list, in the manifestwork, and in the IBGU's status messages, so a failed backup reports an
object the operator can look up directly — `oc get backupspec dpa-config` on
the spoke — rather than a position in a list.

### The options as originally posed, if the pair is not split

Recorded because the spike must show the rejected alternatives, and because one of them
is the fallback if LCA declines ask 1.

| Option | Pros | Cons |
|---|---|---|
| **6A** Cluster-scoped on spoke and hub | Matches IBU's own shape; no spoke namespace prerequisite (drops OADP from `docs/ibgu/README.md:3`); no Namespace manifest needed | Multiple curated sets collide in one flat namespace on the hub; forces name prefixing, and prefixes would have to be rewritten in lockstep with the IBU refs. Largely defused by the library/input split: on the spoke the flat namespace holds only copies, and a new path's Prep overwrites them by name (D8) |
| **6B** Namespaced on spoke and hub (CNF-26659 as written) | Simple, one CRD, hub organisation for free | **Reproduces the fake-namespace hack exactly** — the ref doubles as hub lookup key and spoke target, so the hub needs a phantom namespace purely to make the lookup resolve. The namespaced spoke object itself is sound; it is the ref carrying the namespace that is not, which is what **6B′** changes |
| **6C-as-posed** (early sketch; the label was reused below) Cluster-scoped on spoke, namespaced hub representation | Best of both: clean spoke identity, hub organisation by upgrade path | Scope is baked into a CRD, so this means **two CRDs**, and without ask 1 the hub type duplicates LCA's content schema or embeds an opaque object |

**Recommendation: the two-object split, backed by both asks on CNF-26659.** It is the only
shape that kills the fake-namespace workaround by construction rather than by convention,
and it mirrors the IBGU↔IBU relationship the operator already embodies. Of the two hub-side
forms, **6C with 7B-restricted refs, reusing LCA's own reference field** is the one to
build; 6D (one set ref per upgrade path) is the alternative if sets genuinely need to be
validated as a unit, and it costs a sibling field and a delivery-dependent IBGU schema. On
the spoke side, **6B′ is preferable to 6A** (namespaced, read from a fixed LCA-owned
namespace) for the privilege, migration and reaping reasons above; 6A remains the fallback
if LCA declines to own a read namespace. **Rule out 6B as written** — the phantom namespace
is fatal — but not the namespaced spoke object itself, which 6B′ makes sound.

**Rejected: content inlined in the IBGU spec.** It would delete D6, D7 and D9 outright,
but it fails the requirement that drove the library in the first place — the curated set
is meant to be reused across upgrade paths, and `ibuSpec` is already immutable, so every
content change becomes a new IBGU. Reuse decides this, not scope.

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

**Recommendation: 7B.** The transform on the way out is "strip the metadata", plus "set the
derived name" where it differs (6D) and "set LCA's fixed namespace" under 6B′ — never "carry
the hub namespace to the spoke". Whether a ref may name a namespace other than the IBGU's is
settled by the D6 choice, below; under the recommended form it cannot name one at all.

**Under the D6 reframe this question largely dissolves.** The hub object is a namespaced
library object, and the reference is a name — a `LocalObjectReference` in the recommended
form, or a `NamespacedCR` like `PreCachingConfigCR` (`types.go:46-47`) under 6D. Either way
there is no longer a "hub location is the spoke location" identity to work around, so 7A
stops being the status quo and becomes simply wrong, and 7C's explicit source field is
redundant — the IBGU's own namespace is the source.

What is left is only the namespace rule, and the D6 answer settles it:

- with per-entry objects (6C), take 7B **without** the namespace override, so entries are
  confined to the IBGU's namespace and hub names are unique by construction;
- with a set object (6D), the set ref carries a namespace, because a set is a standalone
  curated artefact and cross-namespace reuse is one of its points.

Under **6B′** the hub-to-spoke mapping is many-to-one, not one-to-one: every hub namespace
writes into the one namespace LCA reads, so 7B governs where the hub *reads* and LCA's
constant governs where the spoke *lands*. The transform on the way out is therefore "strip
the metadata, set the fixed namespace" — no rename under 6C — which is what
`getSecretManifest` already does at `:428`, generalised, and the reason its
namespace-rewriting approach is the one to keep.

---

## D8. Copy semantics and lifecycle on the spoke

**Metadata stripping.** The current helper clears `ResourceVersion`, `ManagedFields`,
`CreationTimestamp`, `UID` (`:449-452`) — right for a ConfigMap. For a CR it should also
strip `ownerReferences` (dangling hub refs can get the object garbage-collected on the
spoke) and `generation`. Given CNF-26659 settles that BackupSpec has no status
subresource and no controller, stale `status` and `finalizers` are not concerns.

**Work-agent RBAC.** The pull-secret copy is not a precedent. Today's copies are core
resources — the Secret into `openshift-lifecycle-agent` (`imagebasedgroupupgrade.go:428`),
the ConfigMaps wherever their ref points — and they apply with no RBAC shipped for them,
so the work agent's default grant evidently covers core namespaced resources. It does not
cover `lca.openshift.io`: TALM already has to ship
`open-cluster-management:klusterlet-work:ibu-role`, aggregated into the agent via
`open-cluster-management.io/aggregate-to-work: "true"` and granting `imagebasedupgrades`
only (`manifestworkreplicaset.go:175-188`; `docs/ibgu/README.md:46`). `BackupSpec` is a
new resource in that same group, so it stands where the IBU stands, not where the Secret
does. Spoke scope does not change this — aggregated ClusterRoles grant cluster-wide, so
6A and 6B′ need the same rule. (The OCM klusterlet manifests are not vendored, so the
agent's exact default grant is inferred from this behaviour rather than read; if LCA
aggregates a `BackupSpec` role into `admin` and the agent is bound to `admin`, it might
already be covered. The explicit rule is correct either way.)

- **Add a `backupspecs` rule to the existing `ibu-role`, not a new manifest.** A separate
  ClusterRole would be one more manifest and shift the completion index, which counts
  manifests (`:120`); an extra rule on the same object does not. Minimum verbs are
  `get`, `create`, `update`, `patch`; `delete` is unnecessary under `Orphan` (8A) but
  harmless, and matching the IBU rule's verb set is simpler. `ibu-role` is already on the
  list restored across the pivot (`docs/ibgu/README.md:12,46`), so the rule carries with
  it.
- **Apply the ClusterRole first.** Manifests are ordered extra manifests, then the
  ClusterRole, then the IBU (`:211-213`). With `BackupSpec` among the extras, the first
  Prep after a TALM upgrade applies it before the role gains the rule and is forbidden;
  the work agent retries, so it converges, but by accident and with `Applied=False`
  showing in the meantime. Moving the ClusterRole to the front keeps the IBU last, so its
  index (`1+len(manifests)`) is unchanged. RBAC aggregation is itself asynchronous, so
  the first attempt can still be refused once; the reorder narrows the window rather
  than closing it. `GeneratePermissionsManifestWorkReplicaset` (`:125`) would ship the
  role ahead of time but has no caller outside tests.
- **Hub-side RBAC is separate and small:** get/list/watch on TALM's own hub kind
  (`BackupContent` or `BackupSet`) via kubebuilder markers. Nothing is needed for LCA's
  `BackupSpec` on the hub — TALM only serialises it into the manifest.
- **Not covered here:** an 8C reap done by `ManagedClusterAction` runs through ACM's
  work-manager addon under its own identity, not the work agent, and its permissions
  would need checking separately.

**Deletion.** Manifests currently carry `DeleteOption: Orphan` (`manifestworkreplicaset.go:161`
and `:233`), so copied content survives the manifestwork.

**The content's lifetime spans Prep *and* Upgrade (pre-pivot), and that decides this
question.** Verified in the controller: only the `Prep` branch copies content —
`getConfigMapManifests` and `getSecretManifest` are called at `:301-302`, while
`GenerateUpgradeManifestWorkReplicaset` (`:309`) carries nothing but the IBU. The
`Upgrade` stage therefore reads whatever `Prep` put on the spoke. Anything that removes
the copies when the Prep manifestwork goes away breaks Upgrade, silently, with no backup
taken and no error until LCA fails at pre-pivot.

| Option | Pros | Cons |
|---|---|---|
| **8A** Keep `Orphan` (status quo) | Content survives Prep→Upgrade as required; also survives the copying IBGU, which a follow-up Upgrade-only IBGU depends on — `[['Upgrade']]` is admitted by the plan CEL (`config/crd/bases/lcm.openshift.io_imagebasedgroupupgrades.yaml:216`) and its selector narrows on prep-completed (`manifestworkreplicaset.go:296-297`), so it never runs Prep and never re-copies | Stale BackupSpecs accumulate; under Orphan, a later set with the same name can overwrite or be mistaken for a leftover — in the cluster under 6A, in the LCA-owned namespace under 6B′ |
| **8B** Delete with the manifestwork (as previously recommended) | No accumulation | **Wrong**: the Prep MWRS is the only carrier, so deleting with it removes Upgrade's input. And the CGU deletes per-cluster ManifestWorks routinely, not only when the IBGU goes: before creating the next template's work (`controllers/manifestwork.go:44-55`), at each new batch (`clustergroupupgrade_controller.go:571-572`), and in its finalizer. Under a `['Prep','Upgrade']` plan item the Prep work — and with it the BackupSpec — would be deleted just before the Upgrade work is created |
| **8C** `Orphan` at the manifestwork, explicit reap at a defined later point | No accumulation, no lifetime accidentally coupled to MWRS GC | Needs a real reap mechanism: `FinalizeUpgrade`/`FinalizeRollback` manifestworks can only apply, so the reap is either a TALM reconcile over leftover spoke objects or a `ManagedClusterAction`; new moving part for a small object |

**Recommendation: 8A, with 8C as an optional follow-up.** Rule out 8B. `Orphan` is not a
convenience here; it is what makes Prep→Upgrade and cross-IBGU resume work at all. The
accumulation concern is real but bounded — one small, inert object per
referenced entry per spoke, with a stable name, so a later IBGU overwrites rather than duplicates, and
under 6B′ the whole accumulation sits in one namespace where an 8C sweep is a single List.

**Two constraints this places on other decisions:**

1. **Derived spoke names must not contain the IBGU name** (updates 6D). Only the `Prep`
   plan item copies content; the `Upgrade` item consumes what is already there. A name
   keyed on the IBGU also breaks as soon as the IBU is re-applied by a different IBGU —
   including the recovery case where the original IBGU is deleted mid-flight and a
   replacement finishes the work. `<set>-<index>` works there; `<ibgu>-<index>` does not.
2. **The namespace is not needed in the name either.** Qualifying the name with the hub
   namespace was proposed to separate same-named entries from different upgrade paths.
   Re-examined, that case cannot arise in a supported flow: every upgrade begins with Prep,
   Prep is what copies the content, and the copy that supplies a given Upgrade is therefore
   written by the same path — overwriting anything left at that name before it is read. A
   new path necessarily re-Preps, so it necessarily overwrites. The only flow in which two
   paths' copies could coexist is concurrent overlap of two IBGUs on one cluster, and that
   is already broken independently of naming: the action labels are IBGU-agnostic
   (`lcm.openshift.io/ibgu-%s-completed`, `constants.go:188`) and nothing rejects the
   overlap.

   So the spoke name is the entry name (6C) — not derived, simply the same string — or
   `<set>-<index>` (6D). This also makes
   recovery work: if the creating IBGU is deleted mid-flight, a replacement IBGU finds the
   same objects because their names depend on neither the IBGU nor its namespace. And it
   removes 8A's accumulation concern for the common case — a later upgrade to a new seed
   image overwrites the previous copies instead of leaving a duplicate behind.

   If concurrent overlap is ever to be supported, qualification comes back and so does the
   need for an ownership guard; the two belong together.

**Open point for D9:** a follow-up Upgrade-only IBGU — one that resumes a path whose
`Prep` ran under an earlier IBGU — may find the referenced hub entries gone while the
spoke copies remain. Either the pre-flight still requires the hub object (fail closed,
small friction on resume) or plans without a `Prep` item are exempt from the existence
check.
Fail-closed is defensible — the IBGU is a record of intent — but it should be a recorded
choice, not an accident.

---

## D9. Behaviour when a referenced BackupSpec cannot be resolved

Today a missing source logs a warning and `continue`s (`:444`), and Prep proceeds. This
is worse than it looks: the completion index is derived from the gathered count —
`1+len(manifests)` at `manifestworkreplicaset.go:120`, pointing at the IBU appended last
(`:211-213`) — so dropping a source *also shifts the expected index to match*, and the
manifestwork reports complete. A typo'd reference yields a green Prep with no backup, and
nothing logs above Info.

| Option | Pros | Cons |
|---|---|---|
| **9A** Hard-fail the IBGU before creating the CGU | Catches typos at the only point where they are cheap to fix | Needs clear per-reference error reporting |
| **9B** Fail the plan item at Prep | Closer to where it is used | Wastes a rollout cycle |
| **9C** Status quo (warn and skip) | — | Silent no-backup. Should not survive this work |

**Recommendation: 9A**, validated alongside the D4 generation check, with the unresolved
reference names in the IBGU condition. Overlaps CNF-28671 but on the hub side of the line.

One case must not be treated as a failed reference: a follow-up Upgrade-only IBGU resuming
a path whose content an earlier IBGU already copied. Nothing is copied at that point, so the
check either applies only to plans carrying a `Prep` item, or it must consult the spoke —
see the open point in D8.

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
   "name + namespace"; the YAML example omits the namespace. These disagree. D6's
   preference is **namespaced, in one namespace LCA owns and exports as a constant, with
   name-only references** (6B′) — a namespace in the ref carries no meaning to LCA and only
   creates a hub-side prerequisite. If LCA will not own and fix a read namespace,
   **cluster-scoped on the spoke with name-only references** (6A) is the fallback; either
   way the ref itself must not carry a namespace. Note one consequence for LCA: under 6C
   the IBGU reuses this field as its own authoring field, so changing its shape in a later
   release becomes a hub-visible API change rather than an internal LCA one.
4. **Can the BackupSpec content be exported as a named spec type** (D6 ask 1), so the hub
   type can embed the same struct instead of duplicating or wrapping it? If not, TALM
   wraps the whole object and both sides carry a schema they have to keep in step.
5. During the release where LCA still serves IBU v1 for conversion, what happens to a v1
   IBU created by the hub — is `oadpContent` readable, or pruned to v2 at rest? The
   ticket's own open item ("confirm the exact pruning behaviour of a None-strategy
   conversion on read") governs whether TALM can ever safely emit v1 to a v2 spoke.
6. Does LCA re-read BackupSpec after Prep (decides D8)?
7. Are the IBU status condition type names stable across the change? (Recorded as an
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
| Copied manifests orphaned on delete | `controllers/utils/manifestworkreplicaset.go:233` |
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
