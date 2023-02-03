package controllers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestClusterGroupUpgradeReconciler_extractPrecachingSpecFromPolicies(t *testing.T) {

	const policyWithOneOperator = `---
    apiVersion: policy.open-cluster-management.io/v1
    kind: Policy
    metadata:
      annotations:
        policy.open-cluster-management.io/categories: CM Configuration Management
        policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
        policy.open-cluster-management.io/standards: NIST SP 800-53
      labels:
        policy.open-cluster-management.io/root-policy: policy6-must-not-have
      name: default.policy6-must-not-have
    spec:
      disabled: false
      policy-templates:
      - objectDefinition:
          apiVersion: policy.open-cluster-management.io/v1
          kind: ConfigurationPolicy
          metadata:
            name: common-must-not-have-policy-config
          spec:
            namespaceselector:
              exclude:
              - kube-*
              include:
              - '*'
            object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: operators.coreos.com/v1alpha1
                kind: Subscription
                metadata:
                  name: performance-addon-operator
                  namespace: openshift-performance-addon-operator
                spec:
                  channel: "4.9"
                  name: performance-addon-operator
                  source: redhat-operators
                  sourceNamespace: openshift-marketplace
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: v1
                kind: Namespace
                metadata:
                  annotations:
                    workload.openshift.io/allowed: management
                  labels:
                    openshift.io/cluster-monitoring: "true"
                  name: openshift-performance-addon-operator
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: operators.coreos.com/v1
                kind: OperatorGroup
                metadata:
                  name: performance-addon-operator
                  namespace: openshift-performance-addon-operator
            remediationAction: inform
            severity: low
      remediationAction: inform
`
	const policyWithClusterVersion = `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  annotations:
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
    policy.open-cluster-management.io/standards: NIST SP 800-53
  name: policy1-common-cluster-version-policy
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: common-cluster-version-policy-config
      spec:
        namespaceselector:
          exclude:
          - kube-*
          include:
          - '*'
        object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: config.openshift.io/v1
            kind: ClusterVersion
            metadata:
              name: PlatformUpgrade
            spec:
              channel: "stable-4.9"
              desiredUpdate:
                force: false
                version: 4.9.8
              upstream: https://api.openshift.com/api/upgrades_info/v1/graph
        remediationAction: inform
        severity: low
  remediationAction: inform
`

	const policyWithMustNotHaveSub = `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  annotations:
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
    policy.open-cluster-management.io/standards: NIST SP 800-53
  name: policy2-common-pao-sub-policy
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: common-pao-sub-policy-config
      spec:
        namespaceselector:
          exclude:
          - kube-*
          include:
          - '*'
        object-templates:
        - complianceType: mustnothave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: Subscription
            metadata:
              name: performance-addon-operator
              namespace: openshift-performance-addon-operator
            spec:
              channel: "4.9"
              name: performance-addon-operator
              source: redhat-operators
              sourceNamespace: openshift-marketplace
        remediationAction: inform
        severity: low
  remediationAction: inform
`

	const policyWithCatSrc = `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  annotations:
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
    policy.open-cluster-management.io/standards: NIST SP 800-53
  labels:
    policy.open-cluster-management.io/root-policy: policy1-common-cluster-version-policy
  name: default.policy0-common-config-policy
# namespace: common-policies
spec:
  disabled: false
  policy-templates:
  - objectDefinition:
      apiVersion: policy.open-cluster-management.io/v1
      kind: ConfigurationPolicy
      metadata:
        name: common-config-policy-config
      spec:
        namespaceselector:
          exclude:
          - kube-*
          include:
          - '*'
        object-templates:
        - complianceType: musthave
          objectDefinition:
            apiVersion: operators.coreos.com/v1alpha1
            kind: CatalogSource
            metadata:
              annotations:
                target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
              name: rh-du-operators
              namespace: openshift-marketplace
            spec:
              displayName: disconnected-redhat-operators
              image: e27-h01-000-r650.rdu2.scalelab.redhat.com:5000/olm-mirror/redhat-operator-index:v4.11
              publisher: Red Hat
              sourceType: grpc
            status:
              connectionState:
                lastObservedState: READY
        remediationAction: inform
        severity: low
  remediationAction: inform

`

	const policyWithMultipleSubscriptions = `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
    annotations:
        policy.open-cluster-management.io/categories: CM Configuration Management
        policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
        policy.open-cluster-management.io/standards: NIST SP 800-53
        ran.openshift.io/ztp-deploy-wave: "2"
    labels:
        policy.open-cluster-management.io/root-policy: policy5-subscriptions
    name: default.policy5-subscriptions
spec:
    remediationAction: inform
    disabled: false
    policy-templates:
        - objectDefinition:
            apiVersion: policy.open-cluster-management.io/v1
            kind: ConfigurationPolicy
            metadata:
                name: common-subscriptions-policy-config
            spec:
                remediationAction: enforce
                severity: low
                namespaceselector:
                    exclude:
                        - kube-*
                    include:
                        - '*'
                object-templates:
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1alpha1
                        kind: Subscription
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: sriov-network-operator-subscription
                            namespace: openshift-sriov-network-operator
                        spec:
                            channel: "4.9"
                            name: sriov-network-operator
                            source: redhat-operators
                            sourceNamespace: openshift-marketplace
                            installPlanApproval: Manual
                        status:
                            state: AtLatestKnown
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: v1
                        kind: Namespace
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                                workload.openshift.io/allowed: management
                            labels:
                                openshift.io/run-level: "1"
                            name: openshift-sriov-network-operator
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1
                        kind: OperatorGroup
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: sriov-network-operators
                            namespace: openshift-sriov-network-operator
                        spec:
                            targetNamespaces:
                                - openshift-sriov-network-operator
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1alpha1
                        kind: Subscription
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: ptp-operator-subscription
                            namespace: openshift-ptp
                        spec:
                            channel: "4.9"
                            name: ptp-operator
                            source: redhat-operators
                            sourceNamespace: openshift-marketplace
                            installPlanApproval: Manual
                        status:
                            state: AtLatestKnown
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: v1
                        kind: Namespace
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                                workload.openshift.io/allowed: management
                            labels:
                                openshift.io/cluster-monitoring: "true"
                            name: openshift-ptp
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1
                        kind: OperatorGroup
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: ptp-operators
                            namespace: openshift-ptp
                        spec:
                            targetNamespaces:
                                - openshift-ptp
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1alpha1
                        kind: Subscription
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: performance-addon-operator
                            namespace: openshift-performance-addon-operator
                        spec:
                            channel: "4.9"
                            name: performance-addon-operator
                            source: redhat-operators
                            sourceNamespace: openshift-marketplace
                            installPlanApproval: Manual
                        status:
                            state: AtLatestKnown
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: v1
                        kind: Namespace
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                                workload.openshift.io/allowed: management
                            labels:
                                openshift.io/cluster-monitoring: "true"
                            name: openshift-performance-addon-operator
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1
                        kind: OperatorGroup
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: performance-addon-operator
                            namespace: openshift-performance-addon-operator
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: v1
                        kind: Namespace
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                                workload.openshift.io/allowed: management
                            name: openshift-logging
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1
                        kind: OperatorGroup
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: cluster-logging
                            namespace: openshift-logging
                        spec:
                            targetNamespaces:
                                - openshift-logging
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1alpha1
                        kind: Subscription
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: cluster-logging
                            namespace: openshift-logging
                        spec:
                            channel: stable
                            name: cluster-logging
                            source: redhat-operators
                            sourceNamespace: openshift-marketplace
                            installPlanApproval: Manual
                        status:
                            state: AtLatestKnown
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: v1
                        kind: Namespace
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                                workload.openshift.io/allowed: management
                            name: openshift-local-storage
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1
                        kind: OperatorGroup
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: openshift-local-storage
                            namespace: openshift-local-storage
                        spec:
                            targetNamespaces:
                                - openshift-local-storage
                    - complianceType: musthave
                      objectDefinition:
                        apiVersion: operators.coreos.com/v1alpha1
                        kind: Subscription
                        metadata:
                            annotations:
                                ran.openshift.io/ztp-deploy-wave: "2"
                            name: local-storage-operator
                            namespace: openshift-local-storage
                        spec:
                            channel: "4.9"
                            installPlanApproval: Manual
                            name: local-storage-operator
                            source: redhat-operators
                            sourceNamespace: openshift-marketplace
                        status:
                            state: AtLatestKnown
    
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
		Log:      logr.Discard(),
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
		want    ranv1alpha1.PrecachingSpec
		wantErr assert.ErrorAssertionFunc
		server  *httptest.Server
	}{
		{
			name:   "With one operator",
			fields: commonFields,
			args:   args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithOneOperator)}},
			want: ranv1alpha1.PrecachingSpec{
				PlatformImage:                "",
				OperatorsIndexes:             []string(nil),
				OperatorsPackagesAndChannels: []string{"performance-addon-operator:4.9"},
				ExcludePrecachePatterns:      []string(nil)},
			wantErr: assert.NoError,
		},
		{
			name:   "With cluster version",
			fields: commonFields,
			args:   args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithClusterVersion)}},
			want: ranv1alpha1.PrecachingSpec{
				PlatformImage:                "quay.io/openshift-release-dev/ocp-release@sha256:c91c0faf7ae3c480724a935b3dab7e5f49aae19d195b12f3a4ae38f8440ea96b",
				OperatorsIndexes:             []string(nil),
				OperatorsPackagesAndChannels: []string(nil),
				ExcludePrecachePatterns:      []string(nil)},
			wantErr: assert.NoError,
		},
		{
			name:   "With mustnothave sub",
			fields: commonFields,
			args:   args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithMustNotHaveSub)}},
			want: ranv1alpha1.PrecachingSpec{
				PlatformImage:                "",
				OperatorsIndexes:             []string(nil),
				OperatorsPackagesAndChannels: []string(nil),
				ExcludePrecachePatterns:      []string(nil)},
			wantErr: assert.NoError,
		},
		{
			name:   "With catloge source",
			fields: commonFields,
			args:   args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithCatSrc)}},
			want: ranv1alpha1.PrecachingSpec{
				PlatformImage:                "",
				OperatorsIndexes:             []string{"e27-h01-000-r650.rdu2.scalelab.redhat.com:5000/olm-mirror/redhat-operator-index:v4.11"},
				OperatorsPackagesAndChannels: []string(nil),
				ExcludePrecachePatterns:      []string(nil)},
			wantErr: assert.NoError,
		},
		{
			name:   "With multiple subscriptions",
			fields: commonFields,
			args:   args{policies: []*unstructured.Unstructured{mustConvertYamlStrToUnstructured(policyWithMultipleSubscriptions)}},
			want: ranv1alpha1.PrecachingSpec{
				PlatformImage:    "",
				OperatorsIndexes: []string(nil),
				OperatorsPackagesAndChannels: []string{
					"sriov-network-operator:4.9", "ptp-operator:4.9", "performance-addon-operator:4.9", "cluster-logging:stable", "local-storage-operator:4.9",
				},
				ExcludePrecachePatterns: []string(nil)},
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

			got, err := r.extractPrecachingSpecFromPolicies(tt.args.policies)
			if !tt.wantErr(t, err, fmt.Sprintf("extractPrecachingSpecFromPolicies(%v)", tt.args.policies)) {
				return
			}
			assert.Equalf(t, tt.want, got, "extractPrecachingSpecFromPolicies(%v)", tt.args.policies)
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
