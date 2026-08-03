package utils

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

func newTestPlacement(clusterNames []string) *unstructured.Unstructured {
	var values []interface{}
	for _, name := range clusterNames {
		values = append(values, name)
	}
	if values == nil {
		values = []interface{}{}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"predicates": []interface{}{
					map[string]interface{}{
						"requiredClusterSelector": map[string]interface{}{
							"labelSelector": map[string]interface{}{
								"matchExpressions": []interface{}{
									map[string]interface{}{
										"key":      "name",
										"operator": "In",
										"values":   values,
									},
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
		placement   *unstructured.Unstructured
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
			name: "returns empty slice when no predicates",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
			expected: []string{},
		},
		{
			name: "returns empty slice when predicates is empty",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"predicates": []interface{}{},
					},
				},
			},
			expected: []string{},
		},
		{
			name: "returns error for invalid predicate structure",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"predicates": []interface{}{"invalid"},
					},
				},
			},
			expectError: true,
		},
		{
			name: "returns empty slice when matchExpressions is missing",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"predicates": []interface{}{
							map[string]interface{}{
								"requiredClusterSelector": map[string]interface{}{
									"labelSelector": map[string]interface{}{},
								},
							},
						},
					},
				},
			},
			expected: []string{},
		},
		{
			name: "returns empty slice when values key is missing",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"predicates": []interface{}{
							map[string]interface{}{
								"requiredClusterSelector": map[string]interface{}{
									"labelSelector": map[string]interface{}{
										"matchExpressions": []interface{}{
											map[string]interface{}{
												"key":      "name",
												"operator": "In",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []string{},
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

func TestSetPlacementClusterNames(t *testing.T) {
	tests := []struct {
		name         string
		placement    *unstructured.Unstructured
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
			expected:     []string{},
		},
		{
			name:         "overwrites existing cluster names",
			placement:    newTestPlacement([]string{"old1", "old2"}),
			clusterNames: []string{"new1", "new2", "new3"},
			expected:     []string{"new1", "new2", "new3"},
		},
		{
			name: "returns error when matchExpression not found",
			placement: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
			clusterNames: []string{"cluster1"},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetPlacementClusterNames(tt.placement, tt.clusterNames)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			result, err := GetPlacementClusterNames(tt.placement)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSetPlacementClusterNamesRoundTrip(t *testing.T) {
	placement := newTestPlacement(nil)

	err := SetPlacementClusterNames(placement, []string{"a", "b", "c"})
	assert.NoError(t, err)

	names, err := GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, names)

	err = SetPlacementClusterNames(placement, []string{"x"})
	assert.NoError(t, err)

	names, err = GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Equal(t, []string{"x"}, names)

	err = SetPlacementClusterNames(placement, nil)
	assert.NoError(t, err)

	names, err = GetPlacementClusterNames(placement)
	assert.NoError(t, err)
	assert.Equal(t, []string{}, names)
}
