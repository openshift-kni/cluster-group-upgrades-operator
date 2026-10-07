package controllers

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				"operators.indexes":             "registry.example.com:5000/redhat-operators:v4.11\n",
				"operators.packagesAndChannels": "ptp-operator:4.9\nsriov-network-operator:4.9\n",
				"excludePrecachePatterns":       "aws\nthanos\n",
				"additionalImages":              "image1:tag\nimage2:tag\n",
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
				"operators.indexes":             "# not-a-comment\n",
				"operators.packagesAndChannels": "operator:4.9\n    extra-key: extra-value\n",
				"excludePrecachePatterns":       "pattern\nother: true\n",
				"additionalImages":              `image:tag", "extra": "field` + "\n",
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

func TestPrecache_additionalImagesPull(t *testing.T) {
	precacheDirectory, err := filepath.Abs("../pre-cache")
	require.NoError(t, err)

	testCases := []struct {
		name          string
		images        []string
		expectedError bool
	}{
		{name: "empty list"},
		{name: "single image", images: []string{"valid:latest"}},
		{name: "multiple images", images: []string{"valid:latest", "other:latest"}},
		{name: "single invalid image", images: []string{"invalid:latest"}, expectedError: true},
		{name: "invalid final image", images: []string{"valid:latest", "invalid:latest"}, expectedError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			action := buildPrecacheSpecConfigMapAction(templateData{AdditionalImages: testCase.images})
			images, found, err := unstructured.NestedString(action.Object, "spec", "kube", "template", "data", "additionalImages")
			require.NoError(t, err)
			require.True(t, found)
			imagesFile := filepath.Join(directory, "additionalImages")
			require.NoError(t, os.WriteFile(imagesFile, []byte(images), 0o600))
			platformFile := filepath.Join(directory, "platformImages")
			require.NoError(t, os.WriteFile(platformFile, nil, 0o600))
			pullLog := filepath.Join(directory, "pull.log")
			require.NoError(t, os.WriteFile(pullLog, nil, 0o600))

			// Mock the container tool so the actual pull script runs without a registry.
			containerTool := filepath.Join(directory, "container-tool")
			require.NoError(t, os.WriteFile(containerTool, []byte(`#!/bin/bash
if [[ $1 == pull ]]; then
    printf '%s\n' "${@: -1}" >> "$PULL_LOG"
    [[ ${@: -1} != invalid:latest ]]
else
    exit 1
fi
`), 0o700))

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "bash", filepath.Join(precacheDirectory, "pull.sh"))
			// Bound output collection if a child process keeps the script's pipes open.
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(), "TEST_ENV=true", "cwd="+precacheDirectory,
				"container_tool="+containerTool, "pull_spec_file="+platformFile,
				"additional_images_spec_file="+imagesFile, "PULL_LOG="+pullLog)
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "pull script must finish before the timeout: %s", output)
			if testCase.expectedError {
				assert.Error(t, err, "invalid images must fail precaching: %s", output)
			} else {
				require.NoError(t, err, "valid images must succeed: %s", output)
			}
			pulls, err := os.ReadFile(pullLog)
			require.NoError(t, err)
			if len(testCase.images) == 0 {
				assert.Empty(t, pulls, "empty lists must not pull images")
			}
			for _, image := range testCase.images {
				assert.Contains(t, strings.Split(string(pulls), "\n"), image, "every additional image must be pulled")
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

	// Scalar values should match the old template exactly.
	for _, key := range []string{"platform.image", "spaceRequired"} {
		assert.Equal(t, oldCMData[key], newCMData[key], "scalar field %q differs", key)
	}

	// For array fields, the old template produces block scalars with leading/trailing
	// whitespace per element. Compare the trimmed, split lines.
	arrayKeys := []string{"operators.indexes", "operators.packagesAndChannels", "excludePrecachePatterns", "additionalImages"}
	for _, key := range arrayKeys {
		oldVal := oldCMData[key].(string)
		newVal := newCMData[key].(string)
		assert.True(t, strings.HasSuffix(newVal, "\n"), "array field %q must end with a newline for shell read loops", key)

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
