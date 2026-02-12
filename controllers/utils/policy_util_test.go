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

	res, err = GetParentPolicyNameAndNamespace("upgrade")
	assert.Error(t, err)

	res, err = GetParentPolicyNameAndNamespace("default.upgrade.cluster")
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
		{
			name: "Templated policy with object-templates-raw",
			inputRawTemplate: `apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: test
  namespace: policies
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: test
      spec:
        object-templates-raw: |
          {{hub range (lookup "v1" "Secret" "policies" "" "").items hub}}
          - complianceType: musthave
            objectDefinition:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: {{hub .metadata.name hub}}
                namespace: policies
              stringData:
                test: ''
          {{hub end hub}}
        pruneObjectBehavior: DeleteIfCreated
        remediationAction: inform
        severity: low
			`,
			expectedResult: `apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: test
  namespace: policies
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: test
      spec:
        object-templates-raw: |

          - complianceType: musthave
            objectDefinition:
              apiVersion: v1
              kind: ConfigMap
              metadata:

                namespace: policies
              stringData:
                test: ''

        pruneObjectBehavior: DeleteIfCreated
        remediationAction: inform
        severity: low`,
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

func TestInspectPolicyObjects(t *testing.T) {
	testcases := []struct {
		name             string
		inputRawTemplate string
		expectedResult   bool
	}{
		{
			name: "Templated policy with object-templates-raw",
			inputRawTemplate: `
          {{hub range (lookup "v1" "Secret" "policies" "" "").items hub}}
          - complianceType: musthave
            objectDefinition:
              apiVersion: v1
              kind: ConfigMap
              metadata:
                name: {{hub .metadata.name hub}}
                namespace: policies
              stringData:
                test: ''
              status:
                state: AtLatestKnown
          {{hub end hub}}`,
			expectedResult: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			policy := &unstructured.Unstructured{}
			policy.SetUnstructuredContent(map[string]interface{}{
				"apiVersion": "policy.open-cluster-management.io/v1",
				"kind":       "Policy",
				"metadata": map[string]interface{}{
					"name":      "test",
					"namespace": "policies",
				},
				"spec": map[string]interface{}{
					"disabled": false,
					"policy-templates": []interface{}{
						map[string]interface{}{
							"objectDefinition": map[string]interface{}{
								"apiVersion": "policy.open-cluster-management.io/v1",
								"kind":       "ConfigurationPolicy",
								"metadata": map[string]interface{}{
									"name": "test",
								},
								"spec": map[string]interface{}{
									"object-templates-raw": tc.inputRawTemplate,
								},
							},
						},
					},
				}})
			actualResult, err := InspectPolicyObjects(policy)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedResult, actualResult)
		})
	}
}
