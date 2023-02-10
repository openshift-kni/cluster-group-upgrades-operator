package controllers

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMCR_renderYamlTemplates(t *testing.T) {
	testcases := []struct {
		name         string
		data         templateData
		template     string
		resourceName string
		result       string
	}{
		{
			name:         "create namespace",
			resourceName: "test-ns",
			data: templateData{
				Cluster:      "test",
				ResourceName: "test-view",
			},
			template: templates.MngClusterActCreatePrecachingNS,
			result: `
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: test-ns
  namespace: test
spec:
  actionType: Create
  kube:
    resource: namespace
    template:
      apiVersion: v1
      kind: Namespace
      metadata:
        name: openshift-talo-pre-cache
        labels:
          pod-security.kubernetes.io/enforce: privileged
        annotations:
          workload.openshift.io/allowed: management
`,
		},
		{
			name:         "create service account",
			resourceName: "test-sa-create",
			data: templateData{
				Cluster:      "test",
				ResourceName: "test-sa",
			},
			template: templates.MngClusterActCreateServiceAcct,
			result: `
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
    name: test-sa-create
    namespace: test
spec:
    actionType: Create
    kube:
        resource: serviceaccount
        template:
            apiVersion: v1
            kind: ServiceAccount
            metadata:
                name: pre-cache-agent
                namespace: openshift-talo-pre-cache
`,
		},
		{
			name:         "create cluster role binding",
			resourceName: "test-crb-create",
			data: templateData{
				Cluster:      "test",
				ResourceName: "test-crb",
			},
			template: templates.MngClusterActCreateClusterRoleBinding,
			result: `
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: test-crb-create
  namespace: test
spec:
  actionType: Create
  kube:
    resource: clusterrolebinding
    template:
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: pre-cache-crb
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: cluster-admin
      subjects:
      - kind: ServiceAccount
        name: pre-cache-agent
        namespace: openshift-talo-pre-cache`,
		},
		{
			name:         "create job",
			resourceName: "test-job-create",
			data: templateData{
				Cluster:       "test",
				ResourceName:  "test-crb",
				WorkloadImage: "test-image",
				JobTimeout:    12,
			},
			template: templates.MngClusterActCreateJob,
			result: `
      apiVersion: action.open-cluster-management.io/v1beta1
      kind: ManagedClusterAction
      metadata:
        name: test-job-create
        namespace: test
      spec:
        actionType: Create
        kube:
          resource: job
          namespace: openshift-talo-pre-cache
          template:
            apiVersion: batch/v1
            kind: Job
            metadata:
              name: pre-cache
              namespace: openshift-talo-pre-cache
              annotations:
                target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
            spec:
              activeDeadlineSeconds: 12
              backoffLimit: 0
              template:
                metadata:
                  name: pre-cache
                  annotations:
                    target.workload.openshift.io/management: '{"effect":"PreferredDuringScheduling"}'
                spec:
                  containers:
                  - args:
                    - /opt/precache/precache.sh
                    command:
                    - /bin/bash
                    - -c
                    env:
                    - name: config_volume_path
                      value: /tmp/precache/config
                    image: test-image
                    name: pre-cache-container
                    resources: {}
                    securityContext:
                      privileged: true
                      runAsUser: 0
                    terminationMessagePath: /dev/termination-log
                    terminationMessagePolicy: File
                    volumeMounts:
                    - mountPath: /host
                      name: host 
                    - mountPath: /etc/config
                      name: config-volume
                      readOnly: true
                  dnsPolicy: ClusterFirst
                  restartPolicy: Never
                  schedulerName: default-scheduler
                  securityContext: {}
                  serviceAccountName: pre-cache-agent
                  priorityClassName: system-cluster-critical
                  volumes:
                  - configMap:
                      defaultMode: 420
                      name: pre-cache-spec
                    name: config-volume
                  - hostPath:
                      path: /
                      type: Directory
                    name: host
`,
		},
		{
			name:         "create job view",
			resourceName: "test-job-view",
			data: templateData{
				Cluster:               "test",
				ResourceName:          "test-view",
				ViewUpdateIntervalSec: 13,
			},
			template: templates.MngClusterViewJob,
			result: `
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: test-job-view
  namespace: test
spec:
  scope:
    resource: jobs
    name: pre-cache
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: 13      
`,
		},
		{
			name:         "create cm view",
			resourceName: "test-cm-view",
			data: templateData{
				Cluster:               "test",
				ResourceName:          "test-cm-view",
				ViewUpdateIntervalSec: 134,
			},
			template: templates.MngClusterViewConfigMap,
			result: `
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: test-cm-view
  namespace: test
spec:
  scope:
    resource: configmap
    name: pre-cache-spec
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: 134`,
		},
		{
			name:         "create sa view",
			resourceName: "test-sa-view",
			data: templateData{
				Cluster:               "test",
				ResourceName:          "test-sa-view",
				ViewUpdateIntervalSec: 14,
			},
			template: templates.MngClusterViewServiceAcct,
			result: `
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: test-sa-view
  namespace: test
spec:
  scope:
    resource: serviceaccounts
    name: pre-cache-agent
    namespace: openshift-talo-pre-cache
    updateIntervalSeconds: 14`,
		},
		{
			name:         "create crb view",
			resourceName: "test-crb-view",
			data: templateData{
				Cluster:               "test",
				ResourceName:          "test-crb-view",
				ViewUpdateIntervalSec: 16,
			},
			template: templates.MngClusterViewClusterRoleBinding,
			result: `
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: test-crb-view
  namespace: test
spec:
  scope:
    resource: clusterrolebinding
    name: pre-cache-crb
    updateIntervalSeconds: 16      
`,
		},
		{
			name:         "create ns view",
			resourceName: "test-ns-view",
			data: templateData{
				Cluster:               "test",
				ResourceName:          "test-ns-view",
				ViewUpdateIntervalSec: 17,
			},
			template: templates.MngClusterViewNamespace,
			result: `
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: test-ns-view
  namespace: test
spec:
  scope:
    resource: namespaces
    name: openshift-talo-pre-cache
    updateIntervalSeconds: 17
`,
		},
		{
			name:         "delete ns",
			resourceName: "delete-ns-action",
			data: templateData{
				Cluster: "test",
			},
			template: templates.MngClusterActDeletePrecachingNS,
			result: `
apiVersion: action.open-cluster-management.io/v1beta1
kind: ManagedClusterAction
metadata:
  name: delete-ns-action
  namespace: test
spec:
  actionType: Delete
  kube:
    resource: namespace
    name: openshift-talo-pre-cache
`,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Log: logr.Discard(),
			}
			obj := &unstructured.Unstructured{}
			w, err := r.renderYamlTemplate(tc.resourceName, tc.template, tc.data)
			if err != nil {
				t.Errorf("error rendering yaml template: %v", err)
			}
			// decode YAML into unstructured.Unstructured
			dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
			renderedObject, _, err := dec.Decode(w.Bytes(), nil, obj)
			if err != nil {
				t.Errorf("error serializing yaml template: %v", err)
			}
			obj1 := &unstructured.Unstructured{}
			desiredObject, _, err := dec.Decode([]byte(tc.result), nil, obj1)
			if err != nil {
				t.Errorf("error serializing yaml string: %v", err)
			}
			assert.Equal(t, true, reflect.DeepEqual(renderedObject, desiredObject))
		})
	}
}

func TestMCR_getView(t *testing.T) {
	testcases := []struct {
		name         string
		data         templateData
		template     string
		resourceName string

		create bool
	}{
		{

			name:         "Create and test view",
			resourceName: "ns-view",
			data: templateData{
				Cluster: "test",
			},
			template: templates.MngClusterViewNamespace,
			create:   true,
		},
		{
			name:         "test non-existing view",
			resourceName: "ns-view",
			data: templateData{
				Cluster: "test",
			},
			template: templates.MngClusterViewNamespace,
			create:   false,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}

			obj := &unstructured.Unstructured{}
			w, err := r.renderYamlTemplate(tc.resourceName, tc.template, tc.data)
			if err != nil {
				t.Errorf("error rendering yaml template: %v", err)
			}
			dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
			_, _, err = dec.Decode(w.Bytes(), nil, obj)
			if err != nil {
				t.Errorf("error serializing yaml template: %v", err)
			}
			if tc.create {
				if err := r.Create(context.TODO(), obj); err != nil {
					t.Errorf("error creating a configmap: %v", err)
				}
			}
			_, found, err := r.getView(context.TODO(), tc.resourceName, tc.data.Cluster)
			if err != nil {
				t.Errorf("error getting a view: %v", err)
			}
			assert.Equal(t, found, tc.create)
		})
	}
}

func TestMCR_createResourcesFromTemplates(t *testing.T) {
	testcases := []struct {
		name         string
		data         templateData
		templates    []resourceTemplate
		resourceName string

		create bool
	}{
		{

			name: "Create objects from precacheDependenciesCreateTemplates",
			data: templateData{
				Cluster: "test",
			},
			templates: precacheDependenciesCreateTemplates,
		},
		{

			name: "Create objects from precacheDependenciesViewTemplates",
			data: templateData{
				Cluster: "test",
			},
			templates: precacheDependenciesViewTemplates,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}

			err := r.createResourcesFromTemplates(context.TODO(), &tc.data, tc.templates)
			if err != nil {
				t.Errorf("error returned from createResourcesFromTemplates: %v", err)
			}
			for _, item := range tc.templates {
				var obj = &unstructured.Unstructured{}
				var kind string
				var group string
				if strings.Contains(item.resourceName, "view") {
					group = "view.open-cluster-management.io"
					kind = "ManagedClusterView"
				} else {
					group = "action.open-cluster-management.io"
					kind = "ManagedClusterAction"
				}
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   group,
					Kind:    kind,
					Version: "v1beta1",
				})
				err = r.Get(context.TODO(), types.NamespacedName{
					Name:      item.resourceName,
					Namespace: tc.data.Cluster,
				}, obj)
				assert.Equal(t, err, nil)
			}
		})
	}
}

func Test_checkViewProcessing(t *testing.T) {
	type args struct {
		viewConditions []interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "Processing true",
			args: args{
				viewConditions: []interface{}{
					map[string]interface{}{
						"type":    viewv1beta1.ConditionViewProcessing,
						"status":  string(metav1.ConditionTrue),
						"message": "Watching resources successfully",
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "Processing false - object not found",
			args: args{
				viewConditions: []interface{}{
					map[string]interface{}{
						"type":    viewv1beta1.ConditionViewProcessing,
						"status":  string(metav1.ConditionFalse),
						"message": `failed to get resource with err: subscriptionstatuses.apps.open-cluster-management.io "acm-policies-sub" not found`,
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "Processing false - cluster unreachable",
			args: args{
				viewConditions: []interface{}{
					map[string]interface{}{
						"type":   viewv1beta1.ConditionViewProcessing,
						"status": string(metav1.ConditionFalse),
						"message": `failed to get resource with err: 
						Get "https://[fd02::1]:443/api/v1/namespaces/openshift-talo-pre-cache/configmaps/pre-cache-spec": dial tcp [fd02::1]:443: connect: connection refused`,
					},
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name:    "Processing condition not found",
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkViewProcessing(tt.args.viewConditions)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkViewProcessing() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkViewProcessing() = %v, want %v", got, tt.want)
			}
		})
	}
}
