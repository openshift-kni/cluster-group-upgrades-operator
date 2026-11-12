package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

// A managed policy whose object-templates-raw cannot be parsed into object
// templates must be reported as invalid by doManagedPoliciesExist rather than
// panic the reconcile.
func TestDoManagedPoliciesExistMalformedRawTemplates(t *testing.T) {
	const ns = "ztp-common"
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"only template expressions", `{{- if true }}{{- end }}`},
		{"a map instead of a list", "complianceType: musthave"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rawJSON := fmt.Sprintf(`{"apiVersion":"policy.open-cluster-management.io/v1","kind":"ConfigurationPolicy",`+
				`"metadata":{"name":"cp"},"spec":{"object-templates-raw":%q}}`, tc.raw)
			templates := []*policiesv1.PolicyTemplate{{ObjectDefinition: runtime.RawExtension{Raw: []byte(rawJSON)}}}
			parent := &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{Name: "raw-policy", Namespace: ns},
				Spec:       policiesv1.PolicySpec{PolicyTemplates: templates},
			}
			child := &policiesv1.Policy{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns + ".raw-policy", Namespace: "spoke1",
					Labels: map[string]string{utils.ChildPolicyLabel: ns + ".raw-policy"},
				},
				Spec: policiesv1.PolicySpec{RemediationAction: policiesv1.Inform, PolicyTemplates: templates},
			}
			c, err := getFakeClientFromObjects(parent, child)
			require.NoError(t, err)
			r := &ClusterGroupUpgradeReconciler{Client: c, Log: logr.Discard(), Scheme: testscheme}
			cgu := &ranv1alpha1.ClusterGroupUpgrade{Spec: ranv1alpha1.ClusterGroupUpgradeSpec{ManagedPolicies: []string{"raw-policy"}}}

			allExist, info, err := r.doManagedPoliciesExist(context.Background(), cgu, []string{"spoke1"})
			require.NoError(t, err)
			assert.False(t, allExist)
			assert.Equal(t, []string{"raw-policy"}, info.invalidPolicies)
			assert.Empty(t, info.presentPolicies)
		})
	}
}
