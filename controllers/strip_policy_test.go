package controllers

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// stripPolicyInput builds a policy object with one ConfigurationPolicy per spec.
func stripPolicyInput(configPolicySpecs ...interface{}) map[string]interface{} {
	var templates []interface{}
	for _, spec := range configPolicySpecs {
		templates = append(templates, map[string]interface{}{
			"objectDefinition": map[string]interface{}{"metadata": map[string]interface{}{"name": "cp"}, "spec": spec},
		})
	}
	return map[string]interface{}{"spec": map[string]interface{}{"policy-templates": templates}}
}

func TestStripPolicy(t *testing.T) {
	clusterVersion := map[string]interface{}{"kind": "ClusterVersion", "spec": map[string]interface{}{"channel": "stable-4.18"}}
	musthave := func(def interface{}) map[string]interface{} {
		return map[string]interface{}{"complianceType": "musthave", "objectDefinition": def}
	}
	rawClusterVersion := `- complianceType: musthave
  objectDefinition:
    kind: ClusterVersion
    spec:
      channel: stable-4.18
`

	tests := []struct {
		name    string
		policy  map[string]interface{}
		want    []map[string]interface{}
		wantErr string
	}{
		{
			name:   "object-templates",
			policy: stripPolicyInput(map[string]interface{}{utils.ObjectTemplates: []interface{}{musthave(clusterVersion)}}),
			want:   []map[string]interface{}{clusterVersion},
		},
		{
			name: "mustnothave objects are filtered out",
			policy: stripPolicyInput(map[string]interface{}{utils.ObjectTemplates: []interface{}{
				map[string]interface{}{"complianceType": "mustnothave", "objectDefinition": map[string]interface{}{"kind": "Namespace"}},
				musthave(clusterVersion),
			}}),
			want: []map[string]interface{}{clusterVersion},
		},
		{
			name:   "object-templates-raw",
			policy: stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: "{{- if true }}\n" + rawClusterVersion + "{{- end }}"}),
			want:   []map[string]interface{}{clusterVersion},
		},
		{
			// InspectPolicyObjects accepts these, so stripPolicy must not reject them.
			name:   "object-templates-raw entries without objectDefinition are skipped",
			policy: stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: "- complianceType: musthave\n- just-a-string\n" + rawClusterVersion}),
			want:   []map[string]interface{}{clusterVersion},
		},
		{
			name:   "object-templates-raw that is empty once stripped has no objects",
			policy: stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: `{{- if true }}{{- end }}`}),
		},
		{
			name:    "object-templates entry without objectDefinition",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplates: []interface{}{map[string]interface{}{"complianceType": "musthave"}}}),
			wantErr: "can't find any objectDefinition",
		},
		{
			name:    "object-templates entry that is not a map",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplates: []interface{}{"just-a-string"}}),
			wantErr: "can't find any objectDefinition",
		},
		{
			name:    "object-templates that is not a list",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplates: map[string]interface{}{"a": "b"}}),
			wantErr: utils.ConfigPlcObjTmplNotList,
		},
		{
			name:    "object-templates-raw that is a map",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: "complianceType: musthave"}),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name:    "object-templates-raw that is not a string",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: int64(1)}),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name:    "object-templates-raw that is not valid YAML",
			policy:  stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: "- a: [unclosed"}),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name: "both object-templates and object-templates-raw",
			policy: stripPolicyInput(map[string]interface{}{
				utils.ObjectTemplates: []interface{}{musthave(clusterVersion)}, utils.ObjectTemplatesRaw: rawClusterVersion,
			}),
			wantErr: "found both",
		},
		{
			name:    "neither object-templates nor object-templates-raw",
			policy:  stripPolicyInput(map[string]interface{}{"severity": "low"}),
			wantErr: "can't find",
		},
		{
			name:    "configuration policy spec that is not a map",
			policy:  stripPolicyInput("not-a-map"),
			wantErr: "objectDefinition.spec not found",
		},
		{
			name:    "no policy-templates",
			policy:  map[string]interface{}{"spec": map[string]interface{}{}},
			wantErr: "policy-templates not found",
		},
		{
			name:    "policy-templates that is not a list",
			policy:  map[string]interface{}{"spec": map[string]interface{}{"policy-templates": "x"}},
			wantErr: "policy-templates is not a list",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stripPolicy(tc.policy)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// A policy that InspectPolicyObjects accepts must not fail the OpenShift
// version validation that runs on every policy CGU.
func TestExtractOCPVersionInfoFromRawTemplatesWithoutObjectDefinition(t *testing.T) {
	policy := &unstructured.Unstructured{Object: stripPolicyInput(map[string]interface{}{utils.ObjectTemplatesRaw: `- complianceType: musthave
- complianceType: musthave
  objectDefinition:
    kind: ClusterVersion
    spec:
      channel: stable-4.18
`})}
	policy.SetName("raw-policy")

	containsStatus, err := utils.InspectPolicyObjects(policy)
	require.NoError(t, err, "the validator accepts this policy")
	assert.False(t, containsStatus)

	info, err := extractOCPVersionInfoFromPolicies([]*unstructured.Unstructured{policy})
	require.NoError(t, err)
	assert.True(t, info.clusterVersionCRFound)
	assert.Equal(t, "stable-4.18", info.channel)
}
