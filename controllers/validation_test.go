package controllers

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	"net/http"
	"net/http/httptest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestClusterGroupUpgradeReconciler_extractOpenshiftImagePlatformFromPolicies(t *testing.T) {

	const policy = `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ClusterVersion
            spec:
              channel: stable-4.11
              desiredUpdate:
                image: "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6"
                version: 4.11.12
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
`
	const policyWithOnlyImage = `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ClusterVersion
            spec:
              desiredUpdate:
                image: "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6"
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
`

	const policyWithOnlyImageAndVersionIsEmptyString = `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ClusterVersion
            spec:
              channel: stable-4.101
              desiredUpdate:
                image: "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6"
                version: ""
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
`

	policyWithOnlyVersion := `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ClusterVersion
            spec:
              channel: stable-4.11
              desiredUpdate:
                version: 4.11.12
              upstream: %s
`

	const policyWithTwoClusterVersion = `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ClusterVersion
            spec:
              channel: stable-4.11
              desiredUpdate:
                image: "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6"
                version: 4.11.12
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
        -  objectDefinition:
            kind: ClusterVersion
            spec:
              channel: stable-4.10 # this will cause the error
              desiredUpdate:
                image: "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6"
                version: 4.11.12
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
`
	const policyWithoutClusterVersion = `---
kind: Policy
spec:
  policy-templates:
  - objectDefinition:
      kind: ConfigurationPolicy
      spec:
        object-templates:
        - objectDefinition:
            kind: ARandomType
            spec:
              channel: stable-4.11
              desiredUpdate:
                version: 4.11.12
              upstream: https://my.api.openshift.com/api/upgrades_info/v100/graph
`
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}

	commonFields := fields{
		// currently this object is not used anywhere but keeping in case there's change in the future
		Client:   nil,
		Log:      nil,
		Scheme:   nil,
		Recorder: nil,
	}

	type args struct {
		policies []*unstructured.Unstructured
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr assert.ErrorAssertionFunc
		server  *httptest.Server
	}{
		{
			name:    "with both Image and Version",
			fields:  commonFields,
			args:    args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policy)}},
			want:    "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6",
			wantErr: assert.NoError,
		},
		{
			name:    "with only Image",
			fields:  commonFields,
			args:    args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithOnlyImage)}},
			want:    "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6",
			wantErr: assert.NoError,
		},
		{
			name:    "with only Image and version is an empty string",
			fields:  commonFields,
			args:    args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithOnlyImageAndVersionIsEmptyString)}},
			want:    "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6",
			wantErr: assert.NoError,
		},
		{
			name:    "with only Version",
			fields:  commonFields,
			want:    "quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6",
			wantErr: assert.NoError,
			server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"version":1,"nodes":[{"version":"4.11.12","payload":"quay.io/openshift-release-dev/ocp-release@sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6","metadata":{"io.openshift.upgrades.graph.previous.remove_regex":"4[.]10[.].*","io.openshift.upgrades.graph.release.channels":"candidate-4.11,fast-4.11,stable-4.11,candidate-4.12","io.openshift.upgrades.graph.release.manifestref":"sha256:0ca14e0f692391970fc23f88188f2a21f35a5ba24fe2f3cb908fd79fa46458e6","url":"https://access.redhat.com/errata/RHSA-2022:7201"}}]}`))
			})),
		},
		{
			name:    "with both Image and Version and conflicting version with in multiple clusterversiongroup",
			fields:  commonFields,
			args:    args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithTwoClusterVersion)}},
			wantErr: assert.Error,
		},
		{
			name:    "return empty string when ClusterVersion is not found",
			fields:  commonFields,
			args:    args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithoutClusterVersion)}},
			want:    "",
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}

			// special case when there needs to be a http call
			if tt.server != nil {
				url := tt.server.URL
				policyWithOnlyVersion = fmt.Sprintf(policyWithOnlyVersion, url)
				tt.args.policies = []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithOnlyVersion)}
			}

			got, err := r.extractOpenshiftImagePlatformFromPolicies(tt.args.policies)
			if !tt.wantErr(t, err, fmt.Sprintf("extractOpenshiftImagePlatformFromPolicies(%v)", tt.args.policies)) {
				return
			}
			assert.Equalf(t, tt.want, got, "extractOpenshiftImagePlatformFromPolicies(%v)", tt.args.policies)
		})
	}
}

// convertYamlStrToUnstructured helper func to convert a CR in Yaml string to Unstructured
func mustConvertYamlStrToUnstructured(cr string) *unstructured.Unstructured {
	jCr, err := yaml.ToJSON([]byte(cr))
	if err != nil {
		panic(err.Error())
	}

	object, err := runtime.Decode(unstructured.UnstructuredJSONScheme, jCr)
	if err != nil {
		panic(err.Error())
	}

	uCr, ok := object.(*unstructured.Unstructured)
	if !ok {
		panic("unstructured.Unstructured expected")
	}
	return uCr
}
