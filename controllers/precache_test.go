package controllers

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

func TestPrecache_parseSpaceRequired(t *testing.T) {
	testCases := []struct {
		name                     string
		spaceRequired            string
		expectedSpaceRequiredGiB string
		expectedError            bool
	}{
		{
			name:                     "invalid space required string",
			spaceRequired:            "abc 123",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "unknown space required format",
			spaceRequired:            "123 ab",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "negative space required value",
			spaceRequired:            "-1 GiB",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "convert byte to GiB",
			spaceRequired:            "1073741824",
			expectedSpaceRequiredGiB: "1",
			expectedError:            false,
		},
		{
			name:                     "convert KB to GiB",
			spaceRequired:            "2500000 KB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert MB to GiB",
			spaceRequired:            "3100 MB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert GB to GiB",
			spaceRequired:            "40 GB",
			expectedSpaceRequiredGiB: "38",
			expectedError:            false,
		},
		{
			name:                     "convert float-valued GB to GiB",
			spaceRequired:            "38.5 GB",
			expectedSpaceRequiredGiB: "36",
			expectedError:            false,
		},
		{
			name:                     "convert TB to GiB",
			spaceRequired:            "2 TB",
			expectedSpaceRequiredGiB: "1863",
			expectedError:            false,
		},
		{
			name:                     "convert PB to GiB",
			spaceRequired:            "1 PB",
			expectedSpaceRequiredGiB: "931323",
			expectedError:            false,
		},
		{
			name:                     "convert KiB to GiB",
			spaceRequired:            "2500000 KiB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert MiB to GiB",
			spaceRequired:            "3100 MiB",
			expectedSpaceRequiredGiB: "4",
			expectedError:            false,
		},
		{
			name:                     "convert GiB to GiB",
			spaceRequired:            "40 GiB",
			expectedSpaceRequiredGiB: "40",
			expectedError:            false,
		},
		{
			name:                     "convert float-valued GiB to GiB",
			spaceRequired:            "38.5 GiB",
			expectedSpaceRequiredGiB: "39",
			expectedError:            false,
		},
		{
			name:                     "convert TiB to GiB",
			spaceRequired:            "2 TiB",
			expectedSpaceRequiredGiB: "2048",
			expectedError:            false,
		},
		{
			name:                     "convert PiB to GiB",
			spaceRequired:            "1 PiB",
			expectedSpaceRequiredGiB: "1048576",
			expectedError:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsedSpaceRequired, err := parseSpaceRequired(tc.spaceRequired)
			if tc.expectedError {
				assert.NotEqual(t, nil, err)
			}
			assert.Equal(t, tc.expectedSpaceRequiredGiB, parsedSpaceRequired)
		})
	}
}

func TestPrecache_buildPrecacheSpecConfigMapAction(t *testing.T) {
	testCases := []struct {
		name     string
		data     templateData
		expected map[string]interface{}
	}{
		{
			name: "basic fields",
			data: templateData{
				Cluster:       "spoke1",
				ResourceName:  "precache-spec-cm-create",
				PlatformImage: "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
				Operators: operatorsData{
					Indexes:             []string{"registry.example.com:5000/redhat-operators:v4.11"},
					PackagesAndChannels: []string{"ptp-operator:4.9", "sriov-network-operator:4.9"},
				},
				ExcludePrecachePatterns: []string{"aws", "thanos"},
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				SpaceRequired:           "45",
			},
			expected: map[string]interface{}{
				"operators.indexes":             "registry.example.com:5000/redhat-operators:v4.11",
				"operators.packagesAndChannels": "ptp-operator:4.9\nsriov-network-operator:4.9",
				"excludePrecachePatterns":       "aws\nthanos",
				"additionalImages":              "image1:tag\nimage2:tag",
				"platform.image":                "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
				"spaceRequired":                 "45",
			},
		},
		{
			name: "empty arrays produce empty strings",
			data: templateData{
				Cluster:       "spoke1",
				ResourceName:  "precache-spec-cm-create",
				PlatformImage: "",
				Operators: operatorsData{
					Indexes:             []string{},
					PackagesAndChannels: []string{},
				},
				ExcludePrecachePatterns: []string{},
				AdditionalImages:        []string{},
				SpaceRequired:           "10",
			},
			expected: map[string]interface{}{
				"operators.indexes":             "",
				"operators.packagesAndChannels": "",
				"excludePrecachePatterns":       "",
				"additionalImages":              "",
				"platform.image":                "",
				"spaceRequired":                 "10",
			},
		},
		{
			name: "special characters in input are preserved as string values",
			data: templateData{
				Cluster:      "spoke1",
				ResourceName: "precache-spec-cm-create",
				Operators: operatorsData{
					PackagesAndChannels: []string{"operator:4.9\n    extra-key: extra-value"},
					Indexes:             []string{"# not-a-comment"},
				},
				ExcludePrecachePatterns: []string{"pattern\nother: true"},
				AdditionalImages:        []string{`image:tag", "extra": "field`},
				SpaceRequired:           "10",
			},
			expected: map[string]interface{}{
				"operators.indexes":             "# not-a-comment",
				"operators.packagesAndChannels": "operator:4.9\n    extra-key: extra-value",
				"excludePrecachePatterns":       "pattern\nother: true",
				"additionalImages":              `image:tag", "extra": "field`,
				"platform.image":                "",
				"spaceRequired":                 "10",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			obj := buildPrecacheSpecConfigMapAction(tc.data)

			assert.Equal(t, "action.open-cluster-management.io/v1beta1", obj.GetAPIVersion())
			assert.Equal(t, "ManagedClusterAction", obj.GetKind())
			assert.Equal(t, tc.data.ResourceName, obj.GetName())
			assert.Equal(t, tc.data.Cluster, obj.GetNamespace())

			spec, found, err := unstructured.NestedMap(obj.Object, "spec")
			assert.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, "Create", spec["actionType"])

			kube := spec["kube"].(map[string]interface{})
			assert.Equal(t, "configmap", kube["resource"])

			tmpl := kube["template"].(map[string]interface{})
			assert.Equal(t, "v1", tmpl["apiVersion"])
			assert.Equal(t, "ConfigMap", tmpl["kind"])

			meta := tmpl["metadata"].(map[string]interface{})
			assert.Equal(t, "pre-cache-spec", meta["name"])
			assert.Equal(t, "openshift-talo-pre-cache", meta["namespace"])

			cmData := tmpl["data"].(map[string]interface{})
			for key, expectedValue := range tc.expected {
				assert.Equal(t, expectedValue, cmData[key], "mismatch for ConfigMap data key %q", key)
			}
		})
	}
}

func TestPrecache_buildPrecacheSpecConfigMapAction_equivalence(t *testing.T) {
	// This is the old template that was removed from precache-templates.go.
	// We keep it here to verify that the new builder produces functionally
	// equivalent ConfigMap data values for the same inputs.
	const oldTemplate = `
{{ template "actionGVK"}}
{{ template "metadata" . }}
spec:
  actionType: Create
  kube:
    resource: configmap
    template:
      apiVersion: v1
      data:
        operators.indexes: |{{ range .Operators.Indexes }}
          {{ . }} {{ end }}
        operators.packagesAndChannels: |{{ range .Operators.PackagesAndChannels }} 
          {{ . }} {{ end }}
        excludePrecachePatterns: |{{ range .ExcludePrecachePatterns }} 
          {{ . }} {{ end }}
        additionalImages: |{{ range .AdditionalImages }}
          {{ . }} {{ end }}
        platform.image: {{ .PlatformImage }}
        spaceRequired: "{{ .SpaceRequired }}"
      kind: ConfigMap
      metadata:
        name: pre-cache-spec
        namespace: openshift-talo-pre-cache
`

	data := templateData{
		Cluster:       "spoke1",
		ResourceName:  "precache-spec-cm-create",
		PlatformImage: "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		Operators: operatorsData{
			Indexes: []string{
				"registry.example.com:5000/redhat-operators:v4.11",
				"registry.example.com:5000/certified-operators:v4.11",
				"registry.example.com:5000/community-operators:v4.11",
			},
			PackagesAndChannels: []string{
				"ptp-operator:4.9",
				"sriov-network-operator:4.9",
				"performance-addon-operator:4.9",
				"local-storage-operator:stable",
				"cluster-logging:stable",
			},
		},
		ExcludePrecachePatterns: []string{"aws", "thanos", "azure", "gcp"},
		AdditionalImages: []string{
			"quay.io/example/app1:v1.0",
			"quay.io/example/app2:v2.0",
			"quay.io/example/app3@sha256:def456",
		},
		SpaceRequired: "45",
	}

	// Render using the old template
	w := new(bytes.Buffer)
	tmpl, err := template.New("old").Parse(templates.CommonTemplates + oldTemplate)
	assert.NoError(t, err)
	err = tmpl.Execute(w, data)
	assert.NoError(t, err)

	oldObj := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err = dec.Decode(w.Bytes(), nil, oldObj)
	assert.NoError(t, err)

	// Build using the new builder
	newObj := buildPrecacheSpecConfigMapAction(data)

	// Extract ConfigMap data from both
	oldKube := oldObj.Object["spec"].(map[string]interface{})["kube"].(map[string]interface{})
	oldCMData := oldKube["template"].(map[string]interface{})["data"].(map[string]interface{})

	newKube := newObj.Object["spec"].(map[string]interface{})["kube"].(map[string]interface{})
	newCMData := newKube["template"].(map[string]interface{})["data"].(map[string]interface{})

	// The old template produces values with trailing whitespace/newlines from the
	// {{ range }} iteration. Trim both sides before comparing to verify functional
	// equivalence (the pre-cache workload trims values when reading them).
	for _, key := range []string{"platform.image", "spaceRequired"} {
		assert.Equal(t, oldCMData[key], newCMData[key], "scalar field %q differs", key)
	}

	// For array fields, the old template produces block scalars with leading/trailing
	// whitespace per element. Compare the trimmed, split lines.
	arrayKeys := []string{"operators.indexes", "operators.packagesAndChannels", "excludePrecachePatterns", "additionalImages"}
	for _, key := range arrayKeys {
		oldVal := strings.TrimSpace(oldCMData[key].(string))
		newVal := strings.TrimSpace(newCMData[key].(string))

		oldLines := splitAndTrim(oldVal)
		newLines := splitAndTrim(newVal)

		assert.Equal(t, oldLines, newLines, "array field %q has different elements", key)
	}

	// Verify structural equivalence of the MCA envelope
	assert.Equal(t, oldObj.GetAPIVersion(), newObj.GetAPIVersion())
	assert.Equal(t, oldObj.GetKind(), newObj.GetKind())
	assert.Equal(t, oldObj.GetName(), newObj.GetName())
	assert.Equal(t, oldObj.GetNamespace(), newObj.GetNamespace())
	assert.Equal(t, oldKube["resource"], newKube["resource"])
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, "\n")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
