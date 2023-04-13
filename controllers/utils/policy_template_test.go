package utils

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	testscheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
}

func TestVerifyHubTemplateFunc(t *testing.T) {

	testcases := []struct {
		name     string
		input    string
		expected error
	}{
		{
			name: "Valid hub templates",
			input: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}'
    test2: '{{hub fromConfigMap "" "common-cm" "common-key" hub}}'
    test3: '{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test4: '{{hub (printf "%s-name" .ManagedClusterName) | fromConfigMap "ztp-common" "common-cm" hub}}'
    test5: '{{hub ( printf "%s-name" .ManagedClusterName ) | fromConfigMap "ztp-common" "common-cm" hub}}'
    test6: '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") hub}}'
    test7: '{{hub ( printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common"  "common-cm" ) hub}}'
    test8: 'test-value-{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test9: '{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}-value'
    test10: |
        {{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}
    test11: |
        {{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}-value
    test12:
        - '{{hub (fromConfigMap "ztp-common" "common-cm" "common-key") | toInt hub}}'
        - "{{hub (fromConfigMap \"ztp-common\" \"common-cm\" \"common-key\") | toBool hub}}"
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") | base64enc hub}}'
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") | toInt hub}}-{{hub fromConfigMap "ztp-common" "common-cm" "common-key" | toInt hub}}'
    test13: '{{hub .ManagedClusterName hub}}'
`,
			expected: nil,
		},
		{
			name: "Unsupported hub template fromSecret",
			input: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromSecret "ztp-common" "common-cm" "common-key" hub}}'
`,
			expected: fmt.Errorf("fromSecret: " + PlcHubTmplFuncErr),
		},
		{
			name: "Unsupported Printf in the Name field in fromConfigMap function",
			input: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromConfigMap "ztp-common" (printf "%s-data" .ManagedClusterName) "common-key" hub}}'
`,
			expected: fmt.Errorf(PlcHubTmplPrinfInNameErr),
		},
		{
			name: "Unsupported Printf in the Namespace field in fromConfigMap function",
			input: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromConfigMap ( printf "%s-data" .ManagedClusterName ) "ztp-common"  "common-key" hub}}'
`,
			expected: fmt.Errorf(PlcHubTmplPrinfInNsErr),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var inputYaml interface{}
			// covert input string to yaml
			if err := yaml.Unmarshal([]byte(tc.input), &inputYaml); err != nil {
				t.Errorf("Unexpected error: %v", err.Error())
			}

			err := VerifyHubTemplateFunctions(inputYaml, "test-policy")
			if err != nil {
				assert.ErrorContains(t, err, tc.expected.Error())
			} else {
				assert.Equal(t, tc.expected, err)
			}
		})
	}
}

func TestProcessHubTemplateFunctions(t *testing.T) {

	testcases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Valid hub templates",
			input: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}'
    test2: '{{hub fromConfigMap "" "common-cm" "common-key" hub}}'
    test3: '{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test4: '{{hub (printf "%s-name" .ManagedClusterName) | fromConfigMap "ztp-common" "common-cm" hub}}'
    test5: '{{hub ( printf "%s-name" .ManagedClusterName ) | fromConfigMap "ztp-common" "common-cm" hub}}'
    test6: '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") hub}}'
    test7: '{{hub ( printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common"  "common-cm" ) hub}}'
    test8: 'test-value-{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test9: '{{hub fromConfigMap "ztp-common" "common-cm" (printf "%s-name" .ManagedClusterName) hub}}-value'
    test10: |
        {{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}
    test11: |
        {{hub fromConfigMap "ztp-common" "common-cm" "common-key" hub}}-value
    test12:
        - '{{hub (fromConfigMap "ztp-common" "common-cm" "common-key") | toInt hub}}'
        - "{{hub (fromConfigMap \"ztp-common\" \"common-cm\" \"common-key\") | toBool hub}}"
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") | base64enc hub}}'
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-common" "common-cm") | toInt hub}}-{{hub fromConfigMap "ztp-common" "common-cm" "common-key" | toInt hub}}'
    test13: '{{hub .ManagedClusterName hub}}'
`,
			expected: `
apiVersion: test.openshift.io/v1
kind: TestResource
metadata:
    name: resource-sample
    namespace: resource-namespace
spec:
    test1: '{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key" hub}}'
    test2: '{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key" hub}}'
    test3: '{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test4: '{{hub (printf "%s-name" .ManagedClusterName) | fromConfigMap "ztp-install" "ztp-common.common-cm" hub}}'
    test5: '{{hub ( printf "%s-name" .ManagedClusterName ) | fromConfigMap "ztp-install" "ztp-common.common-cm" hub}}'
    test6: '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-install" "ztp-common.common-cm") hub}}'
    test7: '{{hub ( printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-install" "ztp-common.common-cm" ) hub}}'
    test8: 'test-value-{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" (printf "%s-name" .ManagedClusterName) hub}}'
    test9: '{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" (printf "%s-name" .ManagedClusterName) hub}}-value'
    test10: |
        {{hub fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key" hub}}
    test11: |
        {{hub fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key" hub}}-value
    test12:
        - '{{hub (fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key") | toInt hub}}'
        - "{{hub (fromConfigMap \"ztp-install\" \"ztp-common.common-cm\" \"common-key\") | toBool hub}}"
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-install" "ztp-common.common-cm") | base64enc hub}}'
        - '{{hub (printf "%s-name" .ManagedClusterName | fromConfigMap "ztp-install" "ztp-common.common-cm") | toInt hub}}-{{hub fromConfigMap "ztp-install" "ztp-common.common-cm" "common-key" | toInt hub}}'
    test13: '{{hub .ManagedClusterName hub}}'
`,
		},
	}

	for _, tc := range testcases {
		objs := []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name:      "common-cm",
					Namespace: "ztp-common",
				},
			},
		}

		r := &TemplateResolver{
			Client:          fake.NewClientBuilder().WithScheme(testscheme).WithObjects(objs...).Build(),
			Ctx:             context.TODO(),
			TargetNamespace: "ztp-install",
			PolicyName:      "resource-sample-ori",
			PolicyNamespace: "ztp-common",
		}

		t.Run(tc.name, func(t *testing.T) {
			var inputYaml interface{}
			// convert input string to yaml
			if err := yaml.Unmarshal([]byte(tc.input), &inputYaml); err != nil {
				t.Errorf("Unexpected error: %v", err.Error())
			}

			actualResult, err := r.ProcessHubTemplateFunctions(inputYaml)
			if err != nil {
				t.Errorf("Unexpected error: %v", err.Error())
			} else {
				var expectedResult interface{}
				// convert expected output string to yaml
				if err := yaml.Unmarshal([]byte(tc.expected), &expectedResult); err != nil {
					t.Errorf("Unexpected error: %v", err.Error())
				}

				assert.Equal(t, expectedResult, actualResult)
			}
		})
	}
}
