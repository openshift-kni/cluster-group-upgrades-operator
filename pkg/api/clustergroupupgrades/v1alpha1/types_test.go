package v1alpha1

import "testing"

func TestRolloutType(t *testing.T) {
	tests := []struct {
		name string
		spec ClusterGroupUpgradeSpec
		want RolloutType
	}{
		{"no templates is a policy rollout", ClusterGroupUpgradeSpec{}, RolloutTypes.Policy},
		{"policies only", ClusterGroupUpgradeSpec{ManagedPolicies: []string{"p1"}}, RolloutTypes.Policy},
		{"empty template list is a policy rollout", ClusterGroupUpgradeSpec{ManifestWorkTemplates: []string{}}, RolloutTypes.Policy},
		{"templates make a manifestwork rollout", ClusterGroupUpgradeSpec{ManifestWorkTemplates: []string{"t1"}}, RolloutTypes.ManifestWork},
		// Today templates win when both are set; see the mutual-exclusion TODO in types.go.
		{"templates win over policies", ClusterGroupUpgradeSpec{ManagedPolicies: []string{"p1"}, ManifestWorkTemplates: []string{"t1"}}, RolloutTypes.ManifestWork},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (ClusterGroupUpgrade{Spec: tc.spec}).RolloutType(); got != tc.want {
				t.Errorf("RolloutType() = %q, want %q", got, tc.want)
			}
		})
	}
}
