package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func init() {
	testscheme.AddKnownTypes(viewv1beta1.GroupVersion, &viewv1beta1.ManagedClusterView{}, &viewv1beta1.ManagedClusterViewList{})
}

const subscriptionAPIVersion = "operators.coreos.com/v1alpha1"

// monitoringPolicy builds a parent policy with one ConfigurationPolicy per spec.
func monitoringPolicy(name string, configPolicySpecs ...map[string]interface{}) *unstructured.Unstructured {
	var templates []interface{}
	for _, spec := range configPolicySpecs {
		def := map[string]interface{}{"metadata": map[string]interface{}{"name": "cp"}}
		if spec != nil {
			def["spec"] = spec
		}
		templates = append(templates, map[string]interface{}{"objectDefinition": def})
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": name},
		"spec":     map[string]interface{}{"policy-templates": templates},
	}}
}

// objectTemplates wraps object templates as a ConfigurationPolicy spec.
func objectTemplates(templates ...interface{}) map[string]interface{} {
	return map[string]interface{}{utils.ObjectTemplates: templates}
}

func rawTemplates(raw interface{}) map[string]interface{} {
	return map[string]interface{}{utils.ObjectTemplatesRaw: raw}
}

// subscriptionTemplate returns a musthave object template for a Subscription;
// drop removes fields from the objectDefinition (or its metadata, as "metadata.<field>").
func subscriptionTemplate(name string, drop ...string) map[string]interface{} {
	metadata := map[string]interface{}{"name": name, "namespace": "openshift-sriov"}
	def := map[string]interface{}{"apiVersion": subscriptionAPIVersion, "kind": "Subscription", "metadata": metadata}
	for _, field := range drop {
		if metadataField, ok := strings.CutPrefix(field, "metadata."); ok {
			delete(metadata, metadataField)
		} else {
			delete(def, field)
		}
	}
	return map[string]interface{}{"complianceType": "musthave", "objectDefinition": def}
}

func subscriptionObject(name string) ConfigurationObject {
	ns := "openshift-sriov"
	return ConfigurationObject{Kind: "Subscription", Name: name, APIVersion: subscriptionAPIVersion, Namespace: &ns}
}

func newMonitoringReconciler(c client.Client) *ClusterGroupUpgradeReconciler {
	return &ClusterGroupUpgradeReconciler{Client: c, Log: logr.Discard(), Scheme: testscheme}
}

func TestGetMonitoredObjects(t *testing.T) {
	mustNotHave := subscriptionTemplate("sriov")
	mustNotHave["complianceType"] = "mustnothave"
	namespace := map[string]interface{}{"complianceType": "musthave", "objectDefinition": map[string]interface{}{
		"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]interface{}{"name": "ns", "namespace": "ns"},
	}}

	tests := []struct {
		name    string
		policy  *unstructured.Unstructured
		want    []ConfigurationObject
		wantErr string
	}{
		{
			name:   "subscription in object-templates",
			policy: monitoringPolicy("p", objectTemplates(subscriptionTemplate("sriov"))),
			want:   []ConfigurationObject{subscriptionObject("sriov")},
		},
		{
			name: "objects from every policy template are collected",
			policy: monitoringPolicy("p",
				objectTemplates(subscriptionTemplate("sriov")),
				objectTemplates(subscriptionTemplate("ptp"), namespace)),
			want: []ConfigurationObject{subscriptionObject("sriov"), subscriptionObject("ptp")},
		},
		{
			name:   "non-subscription kinds are not monitored",
			policy: monitoringPolicy("p", objectTemplates(namespace)),
		},
		{
			name:   "mustnothave objects are skipped",
			policy: monitoringPolicy("p", objectTemplates(mustNotHave)),
		},
		{
			name: "objects missing a required field are skipped",
			policy: monitoringPolicy("p", objectTemplates(
				subscriptionTemplate("no-metadata", "metadata"),
				subscriptionTemplate("no-kind", "kind"),
				subscriptionTemplate("no-name", "metadata.name"),
				subscriptionTemplate("no-namespace", "metadata.namespace"),
				subscriptionTemplate("no-apiversion", "apiVersion"),
				subscriptionTemplate("sriov"),
			)),
			want: []ConfigurationObject{subscriptionObject("sriov")},
		},
		{
			name:   "non-map object template is skipped",
			policy: monitoringPolicy("p", objectTemplates("not-a-map", subscriptionTemplate("sriov"))),
			want:   []ConfigurationObject{subscriptionObject("sriov")},
		},
		{
			name: "subscription in object-templates-raw",
			policy: monitoringPolicy("p", rawTemplates(`{{- range $s := (lookup "v1" "Secret" "ns" "").items }}
- complianceType: musthave
  objectDefinition:
    apiVersion: operators.coreos.com/v1alpha1
    kind: Subscription
    metadata:
      name: sriov
      namespace: openshift-sriov
{{- end }}`)),
			want: []ConfigurationObject{subscriptionObject("sriov")},
		},
		{
			name:   "object-templates-raw entries without objectDefinition are skipped",
			policy: monitoringPolicy("p", rawTemplates("- complianceType: musthave\n- just-a-string\n")),
		},
		{
			name:   "object-templates-raw that is empty has no objects",
			policy: monitoringPolicy("p", rawTemplates(`{{- if true }}{{- end }}`)),
		},
		{
			name:    "object-templates entry without objectDefinition",
			policy:  monitoringPolicy("p", objectTemplates(map[string]interface{}{"complianceType": "musthave"})),
			wantErr: "missing its spec.policy-templates.objectDefinition.spec.object-templates.objectDefinition",
		},
		{
			name:    "object-templates-raw that is a map",
			policy:  monitoringPolicy("p", rawTemplates("complianceType: musthave")),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name:    "object-templates-raw that is not valid YAML",
			policy:  monitoringPolicy("p", rawTemplates("- a: [unclosed")),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name:    "object-templates-raw that is not a string",
			policy:  monitoringPolicy("p", rawTemplates(int64(1))),
			wantErr: utils.ConfigPlcFailRawMarshal,
		},
		{
			name: "both object-templates and object-templates-raw",
			policy: monitoringPolicy("p", map[string]interface{}{
				utils.ObjectTemplates: []interface{}{}, utils.ObjectTemplatesRaw: "- a: b",
			}),
			wantErr: "found both",
		},
		{
			name:    "neither object-templates nor object-templates-raw",
			policy:  monitoringPolicy("p", map[string]interface{}{}),
			wantErr: "can't find",
		},
		{
			name:    "configuration policy without spec",
			policy:  monitoringPolicy("p", nil),
			wantErr: "policy p is missing its spec.policy-templates.objectDefinition.spec",
		},
		{
			name: "policy template without objectDefinition",
			policy: &unstructured.Unstructured{Object: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "p"},
				"spec":     map[string]interface{}{"policy-templates": []interface{}{map[string]interface{}{}}},
			}},
			wantErr: "policy p is missing its spec.policy-templates.objectDefinition",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newMonitoringReconciler(nil).getMonitoredObjects(tc.policy)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsMonitoredObjectType(t *testing.T) {
	assert.True(t, isMonitoredObjectType("Subscription"))
	assert.False(t, isMonitoredObjectType("ClusterVersion"))
	assert.False(t, isMonitoredObjectType("Namespace"))
	assert.False(t, isMonitoredObjectType(nil))
}

func TestProcessManagedPolicyForMonitoredObjects(t *testing.T) {
	cgu := &ranv1alpha1.ClusterGroupUpgrade{Status: ranv1alpha1.ClusterGroupUpgradeStatus{
		ManagedPoliciesContent: map[string]string{},
	}}
	r := newMonitoringReconciler(nil)

	err := r.processManagedPolicyForMonitoredObjects(cgu, []*unstructured.Unstructured{
		monitoringPolicy("with-subscription", objectTemplates(subscriptionTemplate("sriov"))),
		monitoringPolicy("without-subscription", objectTemplates(subscriptionTemplate("x", "kind"))),
	})
	require.NoError(t, err)

	// Policies without monitored objects get no entry, rather than a "null" one.
	require.Len(t, cgu.Status.ManagedPoliciesContent, 1)
	var stored []ConfigurationObject
	require.NoError(t, json.Unmarshal([]byte(cgu.Status.ManagedPoliciesContent["with-subscription"]), &stored))
	assert.Equal(t, []ConfigurationObject{subscriptionObject("sriov")}, stored)

	err = r.processManagedPolicyForMonitoredObjects(cgu, []*unstructured.Unstructured{
		monitoringPolicy("broken", map[string]interface{}{}),
	})
	assert.ErrorContains(t, err, "can't find")
}

func TestProcessMonitoredObjects(t *testing.T) {
	ctx := context.Background()
	content := func(objects ...ConfigurationObject) string {
		b, err := json.Marshal(objects)
		require.NoError(t, err)
		return string(b)
	}
	newCGU := func(policyContent map[string]string) *ranv1alpha1.ClusterGroupUpgrade {
		zero, one := 0, 1
		return &ranv1alpha1.ClusterGroupUpgrade{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cgu", Namespace: "ztp-group",
				Annotations: map[string]string{utils.NameSuffixAnnotation: "abcde"},
			},
			Status: ranv1alpha1.ClusterGroupUpgradeStatus{
				ManagedPoliciesForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "operators"}, {Name: "config"}},
				ManagedPoliciesContent:    policyContent,
				Status: ranv1alpha1.UpgradeStatus{CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
					"spoke1": {State: ranv1alpha1.InProgress, PolicyIndex: &zero},
					"spoke2": {State: ranv1alpha1.Completed, PolicyIndex: &zero},
					"spoke3": {State: ranv1alpha1.InProgress, PolicyIndex: &one},
				}},
			},
		}
	}
	listMCVs := func(t *testing.T, c client.Client) []viewv1beta1.ManagedClusterView {
		list := &viewv1beta1.ManagedClusterViewList{}
		require.NoError(t, c.List(ctx, list))
		return list.Items
	}

	t.Run("creates a view for each monitored object of in-progress clusters", func(t *testing.T) {
		c, err := getFakeClientFromObjects()
		require.NoError(t, err)
		cgu := newCGU(map[string]string{"operators": content(subscriptionObject("sriov"), subscriptionObject("ptp"))})

		require.NoError(t, newMonitoringReconciler(c).processMonitoredObjects(ctx, cgu))

		mcvs := listMCVs(t, c)
		// spoke2 is completed and spoke3's current policy has no monitored objects.
		require.Len(t, mcvs, 2)
		for _, mcv := range mcvs {
			assert.Equal(t, "spoke1", mcv.Namespace)
			assert.Equal(t, "Subscription.operators.coreos.com", mcv.Spec.Scope.Resource)
			assert.Equal(t, "openshift-sriov", mcv.Spec.Scope.Namespace)
			assert.Equal(t, "cgu", mcv.Labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"])
		}
		assert.ElementsMatch(t, []string{"sriov", "ptp"}, []string{mcvs[0].Spec.Scope.Name, mcvs[1].Spec.Scope.Name})

		// A second pass reuses the views.
		require.NoError(t, newMonitoringReconciler(c).processMonitoredObjects(ctx, cgu))
		assert.Len(t, listMCVs(t, c), 2)
	})

	t.Run("cluster version objects get a view and no further processing", func(t *testing.T) {
		c, err := getFakeClientFromObjects()
		require.NoError(t, err)
		ns := ""
		cv := ConfigurationObject{Kind: "ClusterVersion", Name: "version", APIVersion: "config.openshift.io/v1", Namespace: &ns}
		cgu := newCGU(map[string]string{"operators": content(cv)})

		require.NoError(t, newMonitoringReconciler(c).processMonitoredObjects(ctx, cgu))
		mcvs := listMCVs(t, c)
		require.Len(t, mcvs, 1)
		assert.Equal(t, "ClusterVersion.config.openshift.io", mcvs[0].Spec.Scope.Resource)
	})

	t.Run("invalid stored content is an error", func(t *testing.T) {
		c, err := getFakeClientFromObjects()
		require.NoError(t, err)
		err = newMonitoringReconciler(c).processMonitoredObjects(ctx, newCGU(map[string]string{"operators": "not-json"}))
		assert.Error(t, err)
	})

	t.Run("view lookup error is returned", func(t *testing.T) {
		base, err := getFakeClientFromObjects()
		require.NoError(t, err)
		c := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return apierrors.NewInternalError(errors.New("etcd unavailable"))
			},
		})
		err = newMonitoringReconciler(c).processMonitoredObjects(ctx, newCGU(map[string]string{"operators": content(subscriptionObject("sriov"))}))
		assert.ErrorContains(t, err, "etcd unavailable")
	})
}

func TestProcessMonitoredObjectInstallPlanOutcomes(t *testing.T) {
	ctx := context.Background()
	origEnsure := utils.EnsureInstallPlanIsApproved
	t.Cleanup(func() { utils.EnsureInstallPlanIsApproved = origEnsure })

	// A view whose result is a subscription waiting on a manual install plan.
	subscriptionView := func(t *testing.T, cgu *ranv1alpha1.ClusterGroupUpgrade) *viewv1beta1.ManagedClusterView {
		raw, err := json.Marshal(operatorsv1alpha1.Subscription{Status: operatorsv1alpha1.SubscriptionStatus{
			State:   operatorsv1alpha1.SubscriptionStateUpgradePending,
			Install: &operatorsv1alpha1.InstallPlanReference{Name: "install-abc"},
		}})
		require.NoError(t, err)
		mcvName := utils.GetMultiCloudObjectName(cgu, "Subscription", "sriov")
		return &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GetSafeResourceName(mcvName, "", cgu, utils.MaxObjectNameLength),
				Namespace: "spoke1",
			},
			Status: viewv1beta1.ViewStatus{
				Conditions: []metav1.Condition{{
					Type: viewv1beta1.ConditionViewProcessing, Status: metav1.ConditionTrue, Reason: viewv1beta1.ReasonGetResource,
				}},
				Result: runtime.RawExtension{Raw: raw},
			},
		}
	}

	tests := []struct {
		name   string
		result int
		err    error
	}{
		{"install plan approved", utils.InstallPlanWasApproved, nil},
		{"install plan cannot be approved", utils.InstallPlanCannotBeApproved, nil},
		{"multicloud object pending", utils.MultiCloudPendingStatus, nil},
		// Approval errors are logged and do not fail the reconcile.
		{"approval error is not returned", utils.InstallPlanCannotBeApproved, errors.New("approval failed")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := &ranv1alpha1.ClusterGroupUpgrade{ObjectMeta: metav1.ObjectMeta{
				Name: "cgu", Namespace: "ztp-group",
				Annotations: map[string]string{utils.NameSuffixAnnotation: "abcde"},
			}}
			c, err := getFakeClientFromObjects(subscriptionView(t, cgu))
			require.NoError(t, err)

			var calledFor string
			utils.EnsureInstallPlanIsApproved = func(_ context.Context, _ client.Client, _ *ranv1alpha1.ClusterGroupUpgrade,
				sub operatorsv1alpha1.Subscription, clusterName string) (int, error) {
				calledFor = clusterName + "/" + sub.Status.Install.Name
				return tc.result, tc.err
			}

			err = newMonitoringReconciler(c).processMonitoredObject(ctx, cgu, subscriptionObject("sriov"), "spoke1")
			assert.NoError(t, err)
			assert.Equal(t, "spoke1/install-abc", calledFor)
		})
	}
}

// Every policy that InspectPolicyObjects accepts reaches getMonitoredObjects
// (through doManagedPoliciesExist), so getMonitoredObjects must not reject it.
// Fixtures are limited to inputs the validator itself handles without error
// on this branch's base.
func TestGetMonitoredObjectsAcceptsWhatTheValidatorAccepts(t *testing.T) {
	policies := map[string]*unstructured.Unstructured{
		"subscription in object-templates": monitoringPolicy("p", objectTemplates(subscriptionTemplate("sriov"))),
		"subscription in object-templates-raw": monitoringPolicy("p", rawTemplates(`- complianceType: musthave
  objectDefinition:
    apiVersion: operators.coreos.com/v1alpha1
    kind: Subscription
    metadata:
      name: sriov
      namespace: openshift-sriov
`)),
		"raw entry without objectDefinition":  monitoringPolicy("p", rawTemplates("- complianceType: musthave\n")),
		"subscription missing its apiVersion": monitoringPolicy("p", objectTemplates(subscriptionTemplate("x", "apiVersion"))),
		"subscription missing its metadata":   monitoringPolicy("p", objectTemplates(subscriptionTemplate("x", "metadata"))),
	}
	r := newMonitoringReconciler(nil)
	for name, policy := range policies {
		t.Run(name, func(t *testing.T) {
			_, err := utils.InspectPolicyObjects(policy)
			require.NoError(t, err, "fixture must be accepted by the validator")
			_, err = r.getMonitoredObjects(policy)
			assert.NoError(t, err)
		})
	}
}
