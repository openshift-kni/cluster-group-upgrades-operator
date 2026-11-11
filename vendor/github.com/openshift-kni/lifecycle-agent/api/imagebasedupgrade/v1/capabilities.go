/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

// This file holds the capability contract that LCA publishes for a hub consumer
// (TALM/IBGU) to read, so the hub learns what a spoke supports directly rather
// than inferring it from an LCA version. The values below are exported from the
// public API package on purpose: hub consumers depend on them by import rather
// than by copying literals, which is the runtime sibling of the compile-time
// coupling inversion done for the IBU condition types (see ConditionTypes). Once
// shipped, the claim name, the IBU status field name and the value format are a
// stable public contract and must be treated as immutable.

const (
	// SeedFormatVersion is the seed image format version this LCA expects. It is
	// the single source of truth for the version: internal/common re-exports it,
	// and both the published ClusterClaim and the IBU status.seedFormatVersion
	// field carry it so a hub consumer can read a spoke's capability directly.
	//
	// Bump this every time the seed format changes in a backwards incompatible
	// way. It must match the seed image's SeedFormatOCILabel; the spoke's own
	// hard equality check in Prep (controllers/prep_handlers.go) rejects an
	// incompatible seed regardless of what is published here.
	SeedFormatVersion = 4

	// SeedFormatOCILabel is the OCI image label key that carries the seed format
	// version on a seed image. lca-cli stamps SeedFormatVersion onto every seed
	// image it builds under this key, and Prep reads it back to enforce
	// compatibility. It is exported so a hub consumer can read a seed image's
	// format directly, for example to compare it against
	// BackupContentFirstSeedFormatVersion without any spoke read (CNF-29242).
	SeedFormatOCILabel = "com.openshift.lifecycle-agent.seed_format_version"

	// SeedFormatClusterClaimName is the name of the ClusterClaim LCA creates on a
	// managed cluster to publish SeedFormatVersion. OCM syncs the claim into
	// ManagedCluster.status.clusterClaims on the hub, giving the hub a fleet-wide
	// read in a single List.
	//
	// The ".openshift.io" suffix is a hard requirement, not cosmetic: ACM/MCE
	// renders every klusterlet with reservedClusterClaimSuffixes including
	// "openshift.io" (MCE 2.9 / ACM 2.14+), and a claim whose name ends in a
	// reserved suffix is exempt from the per-spoke custom-claim cap
	// (--max-custom-cluster-claims, default 20) and so cannot be truncated or
	// evicted by an unrelated operator's claims. Any future rename must keep the
	// ".openshift.io" suffix.
	SeedFormatClusterClaimName = "seedformatversion.lca.openshift.io"

	// SeedFormatVersionStatusField is the JSON field name of the seed format
	// version on ImageBasedUpgradeStatus. It is exported so a hub consumer can
	// build a ManifestWork status-feedback rule against the field by name.
	SeedFormatVersionStatusField = "seedFormatVersion"

	// BackupContentFirstSeedFormatVersion is the first seed format version that
	// supports the IBU spec.backupContent reference field. It is intentionally
	// not equal to SeedFormatVersion in this release: backupContent ships in the
	// same backport unit as the format bump (CNF-28772), so the first format that
	// supports it is one ahead of the current SeedFormatVersion until that bump
	// lands.
	//
	// LCA itself never compares against this constant. It exists for the hub: it
	// lets TALM reject an IBGU that sets backupContent against a pre-bump seed
	// image by reading the seed image's OCI format label alone, with no spoke
	// read (CNF-29242).
	BackupContentFirstSeedFormatVersion = 5
)
