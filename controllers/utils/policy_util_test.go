package utils

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func init() {
	testscheme.AddKnownTypes(clusterv1beta1.SchemeGroupVersion, &clusterv1beta1.Placement{}, &clusterv1beta1.PlacementList{})
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.PlacementBinding{}, &policiesv1.PlacementBindingList{})
}

func TestGetParentPolicyNameAndNamespace(t *testing.T) {
	res, err := GetParentPolicyNameAndNamespace("default.upgrade")
	assert.NoError(t, err)
	assert.Equal(t, len(res), 2)

	_, err = GetParentPolicyNameAndNamespace("upgrade")
	assert.Error(t, err)

	res, err = GetParentPolicyNameAndNamespace("default.upgrade.cluster")
	assert.NoError(t, err)
	assert.Equal(t, res[0], "default")
	assert.Equal(t, res[1], "upgrade.cluster")
}

func TestShouldSoak(t *testing.T) {
	// no annotation
	res, err := ShouldSoak(&unstructured.Unstructured{}, v1.Now())
	assert.Equal(t, res, false)
	assert.NoError(t, err)

	// annotation present and soak duration is not over
	policy := &unstructured.Unstructured{}
	policy.SetAnnotations(map[string]string{
		SoakAnnotation: "10",
	})
	res, err = ShouldSoak(policy, v1.Now())
	assert.Equal(t, true, res)
	assert.NoError(t, err)

	// annotation present, firstCompliant is zero
	res, err = ShouldSoak(policy, v1.Time{})
	assert.Equal(t, true, res)
	assert.NoError(t, err)

	// annotation present and soak duration is over
	firstCompliantAt := time.Now().Add(time.Duration(-11) * time.Second)
	res, err = ShouldSoak(policy, v1.NewTime(firstCompliantAt))
	assert.Equal(t, false, res)
	assert.NoError(t, err)

	// annotation present, soak duration is invalid
	policy.SetAnnotations(map[string]string{
		SoakAnnotation: "-1",
	})
	_, err = ShouldSoak(policy, v1.Now())
	assert.Error(t, err)
}

func TestUpdateManagedPolicyNamespaceList(t *testing.T) {
	testcases := []struct {
		policiesNs     map[string][]string
		policyNameArr  []string
		expectedResult map[string][]string
	}{
		{
			policiesNs: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
			policyNameArr: []string{"bbb", "policy2"},
			expectedResult: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa", "bbb"},
			},
		},
		{
			policiesNs:    map[string][]string{},
			policyNameArr: []string{"bbb", "policy2"},
			expectedResult: map[string][]string{
				"policy2": {"bbb"},
			},
		},
		{
			policiesNs: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
			policyNameArr: []string{"aaa", "policy1"},
			expectedResult: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
		},
	}

	for _, tc := range testcases {
		UpdateManagedPolicyNamespaceList(tc.policiesNs, tc.policyNameArr)
		assert.Equal(t, tc.policiesNs, tc.expectedResult)
	}
}

func TestStripObjectTemplatesRaw(t *testing.T) {
	// Test cases were derived from examples on this blog post: https://cloud.redhat.com/blog/tips-for-using-templating-in-governance-policies-part-2
	testcases := []struct {
		name             string
		inputRawTemplate string
		expectedResult   string
	}{
		{
			name: "Singleline ACM Templating",
			inputRawTemplate: `
				{{- /* find Portworx pods in terminated state */ -}}
				{{- range $pp := (lookup "v1" "Pod" "portworx" "").items }}
				{{- /* if the pod is blocked because it is in node shutdown we should delete the pod */ -}}
				{{- if and (eq $pp.status.phase "Failed") (contains "kvdb" $pp.metadata.name) }}
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: {{ $pp.metadata.name }}
      namespace: {{ $pp.metadata.namespace }}
				{{- end }}
				{{- end }}
			`,
			expectedResult: `
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: placeholder
      namespace: placeholder
			`,
		},
		{
			name: "Multiline ACM Templating",
			inputRawTemplate: `
				{{- /* find Portworx pods in terminated state */ -}}
				{{- range $pp := (lookup "v1" "Pod" "portworx" "").items }}
				{{- /* if the pod is blocked because it is in node shutdown we should delete the pod */ -}}
				{{- if and (eq $pp.status.phase "Failed")
							(contains "kvdb" $pp.metadata.name) }}
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: {{ $pp.metadata.name }}
      namespace: {{ $pp.metadata.namespace }}
				{{- end }}
				{{- end }}
			`,
			expectedResult: `
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: placeholder
      namespace: placeholder
			`,
		},
		{
			name: "Singleline ACM Templating and Hub Templating",
			inputRawTemplate: `
				{{- /* find Portworx pods in terminated state */ -}}
				{{- range $pp := (lookup "v1" "Pod" "portworx" "").items }}
				{{- /* if the pod is blocked because it is in node shutdown we should delete the pod */ -}}
				{{- if and (eq $pp.status.phase "Failed") (contains "kvdb" $pp.metadata.name) }}
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: {{ $pp.metadata.name }}-{{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}
      namespace: {{ $pp.metadata.namespace }}
				{{- end }}
				{{- end }}
			`,
			expectedResult: `
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: placeholder-
      namespace: placeholder
			`,
		},
		{
			name: "Multiline ACM Templating and Hub Templating",
			inputRawTemplate: `
				{{- /* find Portworx pods in terminated state */ -}}
				{{- range $pp := (lookup "v1" "Pod" "portworx" "").items }}
				{{- /* if the pod is blocked because it is in node shutdown we should delete the pod */ -}}
				{{- if and (eq $pp.status.phase "Failed")
					(contains "kvdb" $pp.metadata.name) }}
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: {{ $pp.metadata.name }}-{{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}
      namespace: {{ $pp.metadata.namespace }}
				{{- end }}
				{{- end }}
			`,
			expectedResult: `
- complianceType: mustnothave
  objectDefinition:
    apiVersion: v1
    kind: Pod
    metadata:
      name: placeholder-
      namespace: placeholder
			`,
		},
		{
			name: "Inline range function template",
			inputRawTemplate: `
			{{- range (lookup "v1" "ConfigMap" "default" "").items }}
			{{- if eq .data.name "Sea Otter" }}
- complianceType: musthave
  objectDefinition:
    kind: ConfigMap
    apiVersion: v1
    metadata:
      name: {{ .metadata.name }}
      namespace: {{ .metadata.namespace }}
      labels:
        species-category: mammal
			{{- end }}
			{{- end }}
            `,
			expectedResult: `
- complianceType: musthave
  objectDefinition:
    kind: ConfigMap
    apiVersion: v1
    metadata:
      name: placeholder
      namespace: placeholder
      labels:
        species-category: mammal
            `,
		},
		{
			name: "Vertical bar multiline string template",
			inputRawTemplate: `|
{{- range (lookup "v1" "ConfigMap" "default" "").items }}
{{- if eq .data.name "Sea Otter" }}
- complianceType: musthave
  objectDefinition:
    kind: ConfigMap
    apiVersion: v1
    metadata:
      name: {{ .metadata.name }}
      namespace: {{ .metadata.namespace }}
      labels:
        species-category: mammal
{{- end }}
{{- end }}
            `,
			expectedResult: `|


- complianceType: musthave
  objectDefinition:
    kind: ConfigMap
    apiVersion: v1
    metadata:
      name: placeholder
      namespace: placeholder
      labels:
        species-category: mammal
`,
		},
		{
			name: "Hub side templated policy",
			inputRawTemplate: `|
{{hub if $myConfigMap := (lookup "v1" "ConfigMap" "default" $configMap.name) hub}}
{{hub range $key, $value := $myConfigMap.data hub}}
- complianceType: mustonlyhave
	objectDefinition:
		{{hub $value | indent 14 hub}}
{{hub end hub}}
{{hub end hub}}
			`,
			expectedResult: `|


- complianceType: mustonlyhave
	objectDefinition:
`,
		},
	}

	// Loop over all test cases
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Call test function
			actualResult := StripObjectTemplatesRaw(tc.inputRawTemplate)

			// Trim whitespace
			trimmedActual := strings.TrimSpace(actualResult)
			trimmedExpected := strings.TrimSpace(tc.expectedResult)

			// Assert the actual result matches the expected result
			assert.Equal(t, trimmedActual, trimmedExpected)
		})
	}
}

func newTestPlacement(clusterNames []string) *clusterv1beta1.Placement {
	values := clusterNames
	if values == nil {
		values = []string{}
	}

	return &clusterv1beta1.Placement{
		Spec: clusterv1beta1.PlacementSpec{
			Predicates: []clusterv1beta1.ClusterPredicate{
				{
					RequiredClusterSelector: clusterv1beta1.ClusterSelector{
						LabelSelector: v1.LabelSelector{
							MatchExpressions: []v1.LabelSelectorRequirement{
								{
									Key:      "name",
									Operator: v1.LabelSelectorOpIn,
									Values:   values,
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestGetPlacementClusterNames(t *testing.T) {
	tests := []struct {
		name        string
		placement   *clusterv1beta1.Placement
		expected    []string
		expectError bool
	}{
		{
			name:      "returns cluster names from valid placement",
			placement: newTestPlacement([]string{"cluster1", "cluster2", "cluster3"}),
			expected:  []string{"cluster1", "cluster2", "cluster3"},
		},
		{
			name:      "returns empty slice for placement with no clusters",
			placement: newTestPlacement(nil),
			expected:  []string{},
		},
		{
			name:        "returns error when no predicates",
			placement:   &clusterv1beta1.Placement{},
			expectError: true,
		},
		{
			name: "returns error when matchExpressions is empty",
			placement: &clusterv1beta1.Placement{
				Spec: clusterv1beta1.PlacementSpec{
					Predicates: []clusterv1beta1.ClusterPredicate{
						{
							RequiredClusterSelector: clusterv1beta1.ClusterSelector{
								LabelSelector: v1.LabelSelector{},
							},
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetPlacementClusterNames(tt.placement)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetPlacementClusterNamesDefensiveCopy(t *testing.T) {
	placement := newTestPlacement([]string{"cluster1", "cluster2"})
	result, err := GetPlacementClusterNames(placement)
	assert.NoError(t, err)

	result[0] = "modified"
	original := placement.Spec.Predicates[0].RequiredClusterSelector.LabelSelector.MatchExpressions[0].Values
	assert.Equal(t, "cluster1", original[0], "modifying returned slice should not alter placement internals")
}

func TestSetPlacementClusterNames(t *testing.T) {
	tests := []struct {
		name         string
		placement    *clusterv1beta1.Placement
		clusterNames []string
		expected     []string
		expectError  bool
	}{
		{
			name:         "sets cluster names on valid placement",
			placement:    newTestPlacement(nil),
			clusterNames: []string{"cluster1", "cluster2"},
			expected:     []string{"cluster1", "cluster2"},
		},
		{
			name:         "clears cluster names with nil",
			placement:    newTestPlacement([]string{"cluster1"}),
			clusterNames: nil,
			expected:     nil,
		},
		{
			name:         "overwrites existing cluster names",
			placement:    newTestPlacement([]string{"old1", "old2"}),
			clusterNames: []string{"new1", "new2", "new3"},
			expected:     []string{"new1", "new2", "new3"},
		},
		{
			name:         "returns error when no predicates",
			placement:    &clusterv1beta1.Placement{},
			clusterNames: []string{"cluster1"},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetPlacementClusterNames(tt.placement, tt.clusterNames)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				result, err := GetPlacementClusterNames(tt.placement)
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetSetPlacementClusterNamesRoundTrip(t *testing.T) {
	placement := newTestPlacement(nil)

	assert.NoError(t, SetPlacementClusterNames(placement, []string{"a", "b", "c"}))
	names, err := GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, names)

	assert.NoError(t, SetPlacementClusterNames(placement, []string{"x"}))
	names, err = GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Equal(t, []string{"x"}, names)

	assert.NoError(t, SetPlacementClusterNames(placement, nil))
	names, err = GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Nil(t, names)
}

// policyWithTemplate builds a parent policy whose single policy template is a
// ConfigurationPolicy with the given objectDefinition.spec.
func policyWithTemplate(configPolicySpec map[string]interface{}) *unstructured.Unstructured {
	objectDefinition := map[string]interface{}{"metadata": map[string]interface{}{"name": "cp"}}
	if configPolicySpec != nil {
		objectDefinition["spec"] = configPolicySpec
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "parent"},
		"spec": map[string]interface{}{"policy-templates": []interface{}{
			map[string]interface{}{"objectDefinition": objectDefinition},
		}},
	}}
}

func objectTemplate(status bool) map[string]interface{} {
	def := map[string]interface{}{"kind": "Subscription"}
	if status {
		def["status"] = map[string]interface{}{"state": "AtLatestKnown"}
	}
	return map[string]interface{}{"complianceType": "musthave", "objectDefinition": def}
}

func TestInspectPolicyObjects(t *testing.T) {
	tests := []struct {
		name       string
		policy     *unstructured.Unstructured
		wantStatus bool
		wantErrMsg string
	}{
		{
			name:   "object-templates without status",
			policy: policyWithTemplate(map[string]interface{}{ObjectTemplates: []interface{}{objectTemplate(false)}}),
		},
		{
			name:       "object-templates with status",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplates: []interface{}{objectTemplate(false), objectTemplate(true)}}),
			wantStatus: true,
		},
		{
			name:       "object-templates that is not a list",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplates: map[string]interface{}{"complianceType": "musthave"}}),
			wantErrMsg: ConfigPlcObjTmplNotList,
		},
		{
			name:   "object-templates entry that is not a map",
			policy: policyWithTemplate(map[string]interface{}{ObjectTemplates: []interface{}{"just-a-string"}}),
			// Unlike object-templates-raw, a structured object-templates entry must define an objectDefinition.
			wantErrMsg: ConfigPlcMissObjTmplDef,
		},
		{
			name:       "object-templates entry without objectDefinition",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplates: []interface{}{map[string]interface{}{"complianceType": "musthave"}}}),
			wantErrMsg: ConfigPlcMissObjTmplDef,
		},
		{
			name: "object-templates-raw with status",
			policy: policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: `{{- range $s := (lookup "v1" "Secret" "ns" "").items }}
- complianceType: musthave
  objectDefinition:
    kind: Subscription
    status:
      state: AtLatestKnown
{{- end }}`}),
			wantStatus: true,
		},
		{
			name:   "object-templates-raw entries without objectDefinition are skipped",
			policy: policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: "- complianceType: musthave\n- just-a-string\n"}),
		},
		{
			name:       "object-templates-raw that is empty",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: ""}),
			wantErrMsg: ConfigPlcRawObjTmplErr,
		},
		{
			name:       "object-templates-raw made only of templates",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: `{{- if true }}{{- end }}`}),
			wantErrMsg: ConfigPlcRawObjTmplErr,
		},
		{
			name:       "object-templates-raw that is a map, not a list",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: "complianceType: musthave"}),
			wantErrMsg: ConfigPlcFailRawMarshal,
		},
		{
			name:       "object-templates-raw that is not valid YAML",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: "- a: [unclosed"}),
			wantErrMsg: ConfigPlcFailRawMarshal,
		},
		{
			name:       "object-templates-raw that is not a string",
			policy:     policyWithTemplate(map[string]interface{}{ObjectTemplatesRaw: int64(1)}),
			wantErrMsg: ConfigPlcFailRawMarshal,
		},
		{
			name: "both object-templates and object-templates-raw",
			policy: policyWithTemplate(map[string]interface{}{
				ObjectTemplates: []interface{}{objectTemplate(false)}, ObjectTemplatesRaw: "- a: b",
			}),
			wantErrMsg: ConfigPlcHasBothObjTmpl,
		},
		{
			name:       "neither object-templates nor object-templates-raw",
			policy:     policyWithTemplate(map[string]interface{}{"severity": "low"}),
			wantErrMsg: ConfigPlcMissAnyObjTmpl,
		},
		{
			name:       "configuration policy without spec",
			policy:     policyWithTemplate(nil),
			wantErrMsg: PlcMissTmplDefSpec,
		},
		{
			name: "configuration policy without metadata",
			policy: &unstructured.Unstructured{Object: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "parent"},
				"spec": map[string]interface{}{"policy-templates": []interface{}{
					map[string]interface{}{"objectDefinition": map[string]interface{}{"spec": map[string]interface{}{}}},
				}},
			}},
			wantErrMsg: PlcMissTmplDefMeta,
		},
		{
			name: "policy template without objectDefinition",
			policy: &unstructured.Unstructured{Object: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "parent"},
				"spec":     map[string]interface{}{"policy-templates": []interface{}{map[string]interface{}{}}},
			}},
			wantErrMsg: PlcMissTmplDef,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			containsStatus, err := InspectPolicyObjects(tc.policy)
			if tc.wantErrMsg == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.wantStatus, containsStatus)
				return
			}
			var policyErr *PolicyErr
			require.True(t, errors.As(err, &policyErr), "want *PolicyErr, got %v", err)
			assert.Equal(t, "parent", policyErr.ObjName)
			assert.Equal(t, tc.wantErrMsg, policyErr.ErrMsg)
			assert.Equal(t, "parent: "+tc.wantErrMsg, err.Error())
		})
	}
}

func TestStringToYaml(t *testing.T) {
	got, err := StringToYaml("- a: 1\n- b")
	require.NoError(t, err)
	assert.Equal(t, []interface{}{map[string]interface{}{"a": 1}, "b"}, got)

	_, err = StringToYaml("a: [unclosed")
	assert.ErrorContains(t, err, "could not unmarshal data")
}

func TestGetResourceName(t *testing.T) {
	cgu := &ranv1alpha1.ClusterGroupUpgrade{ObjectMeta: v1.ObjectMeta{Name: "My-CGU"}}
	assert.Equal(t, "my-cgu-common-policy", GetResourceName(cgu, "Common-Policy"))
}

func newPolicy(name, namespace string, labels map[string]string) *policiesv1.Policy {
	return &policiesv1.Policy{ObjectMeta: v1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
}

func TestGetChildPolicies(t *testing.T) {
	childLabels := map[string]string{ChildPolicyLabel: "ztp-common.common-config"}
	objs := []client.Object{
		newPolicy("ztp-common.common-config", "spoke1", childLabels),
		newPolicy("ztp-common.common-sub", "spoke2", map[string]string{ChildPolicyLabel: "ztp-common.common-sub"}),
		// Child of a CGU-copied policy: skipped.
		newPolicy("ztp-install.cgu-common-config", "spoke1", map[string]string{
			ChildPolicyLabel: "ztp-install.cgu-common-config",
			"openshift-cluster-group-upgrades/clusterGroupUpgrade": "cgu",
		}),
		// No labels, or no root-policy label: skipped.
		newPolicy("unlabeled", "spoke1", nil),
		newPolicy("other", "spoke1", map[string]string{"app": "x"}),
		// A cluster that was not asked for.
		newPolicy("ztp-common.common-config", "spoke3", childLabels),
	}
	c, err := getFakeClientFromObjects(objs...)
	require.NoError(t, err)

	got, err := GetChildPolicies(context.Background(), c, []string{"spoke1", "spoke2", "spoke-without-policies"})
	require.NoError(t, err)
	var names []string
	for _, p := range got {
		names = append(names, p.Namespace+"/"+p.Name)
	}
	assert.ElementsMatch(t, []string{"spoke1/ztp-common.common-config", "spoke2/ztp-common.common-sub"}, names)

	none, err := GetChildPolicies(context.Background(), c, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestDeletePolicyObjects(t *testing.T) {
	ownedLabels := map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": "cgu"}
	otherLabels := map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": "other"}
	meta := func(name, ns string, labels map[string]string) v1.ObjectMeta {
		return v1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}
	}
	objs := []client.Object{
		newPolicy("owned", "ns", ownedLabels),
		newPolicy("other", "ns", otherLabels),
		newPolicy("owned-elsewhere", "ns2", ownedLabels),
		&clusterv1beta1.Placement{ObjectMeta: meta("owned", "ns", ownedLabels)},
		&clusterv1beta1.Placement{ObjectMeta: meta("other", "ns", otherLabels)},
		&policiesv1.PlacementBinding{ObjectMeta: meta("owned", "ns", ownedLabels)},
		&policiesv1.PlacementBinding{ObjectMeta: meta("other", "ns", otherLabels)},
	}
	c, err := getFakeClientFromObjects(objs...)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, DeletePolicies(ctx, c, "ns", ownedLabels))
	require.NoError(t, DeletePlacements(ctx, c, "ns", ownedLabels))
	require.NoError(t, DeletePlacementBindings(ctx, c, "ns", ownedLabels))
	// Deleting again when nothing matches is not an error.
	require.NoError(t, DeletePolicies(ctx, c, "ns", ownedLabels))
	require.NoError(t, DeletePlacements(ctx, c, "ns", ownedLabels))
	require.NoError(t, DeletePlacementBindings(ctx, c, "ns", ownedLabels))

	exists := func(obj client.Object, ns, name string) bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, obj)
		if apierrors.IsNotFound(err) {
			return false
		}
		require.NoError(t, err)
		return true
	}
	assert.False(t, exists(&policiesv1.Policy{}, "ns", "owned"))
	assert.True(t, exists(&policiesv1.Policy{}, "ns", "other"))
	assert.True(t, exists(&policiesv1.Policy{}, "ns2", "owned-elsewhere"))
	assert.False(t, exists(&clusterv1beta1.Placement{}, "ns", "owned"))
	assert.True(t, exists(&clusterv1beta1.Placement{}, "ns", "other"))
	assert.False(t, exists(&policiesv1.PlacementBinding{}, "ns", "owned"))
	assert.True(t, exists(&policiesv1.PlacementBinding{}, "ns", "other"))
}

func TestClientHelperErrors(t *testing.T) {
	ctx := context.Background()
	errBoom := apierrors.NewInternalError(errors.New("etcd unavailable"))
	base, err := getFakeClientFromObjects(newPolicy("owned", "ns", map[string]string{"a": "b"}))
	require.NoError(t, err)
	failing := interceptor.NewClient(base, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return errBoom },
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return errBoom
		},
		DeleteAllOf: func(context.Context, client.WithWatch, client.Object, ...client.DeleteAllOfOption) error {
			return errBoom
		},
	})
	failingDelete := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return errBoom
		},
	})
	cgu := newMWTestCGU()
	cgu.Status.RemediationPlan = [][]string{{"spoke1"}}

	tests := []struct {
		name string
		call func() error
	}{
		{"GetChildPolicies list", func() error { _, err := GetChildPolicies(ctx, failing, []string{"spoke1"}); return err }},
		{"DeletePolicies list", func() error { return DeletePolicies(ctx, failing, "ns", nil) }},
		{"DeletePolicies delete", func() error { return DeletePolicies(ctx, failingDelete, "ns", nil) }},
		{"DeletePlacements", func() error { return DeletePlacements(ctx, failing, "ns", nil) }},
		{"DeletePlacementBindings", func() error { return DeletePlacementBindings(ctx, failing, "ns", nil) }},
		{"CleanupManifestWorkForBatch", func() error { return CleanupManifestWorkForBatch(ctx, failing, cgu, 0) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, tc.call(), "etcd unavailable")
		})
	}
}
