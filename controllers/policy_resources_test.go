package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.PlacementBinding{}, &policiesv1.PlacementBindingList{})
}

const (
	plcNamespace          = "ztp-common"
	plcTestCGULabel       = "openshift-cluster-group-upgrades/clusterGroupUpgrade"
	plcTestCGUNsLabel     = "openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace"
	plcTestForPolicyLabel = "openshift-cluster-group-upgrades/forPolicy"
	configPolicyJSON      = `{"apiVersion":"policy.open-cluster-management.io/v1","kind":"ConfigurationPolicy",` +
		`"metadata":{"name":"cp"},"spec":{"object-templates":[{"complianceType":"musthave",` +
		`"objectDefinition":{"kind":"ConfigMap","metadata":{"name":"cm","namespace":"ns"}}}]}}`
)

var errEtcd = apierrors.NewInternalError(errors.New("etcd unavailable"))

func newPolicyReconciler(t *testing.T, c client.Client) *ClusterGroupUpgradeReconciler {
	t.Helper()
	return &ClusterGroupUpgradeReconciler{Client: c, Log: logr.Discard(), Scheme: testscheme}
}

func newPolicyCGU(policies ...string) *ranv1alpha1.ClusterGroupUpgrade {
	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cgu", Namespace: "ztp-install",
			Annotations: map[string]string{utils.NameSuffixAnnotation: "abcde"},
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{ManagedPolicies: policies, Clusters: []string{"spoke1", "spoke2"}},
	}
}

// rootPolicy returns a parent policy with a valid ConfigurationPolicy and the given per-cluster compliance.
func rootPolicy(name string, compliance map[string]policiesv1.ComplianceState) *policiesv1.Policy {
	p := &policiesv1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: plcNamespace},
		Spec: policiesv1.PolicySpec{PolicyTemplates: []*policiesv1.PolicyTemplate{
			{ObjectDefinition: runtime.RawExtension{Raw: []byte(configPolicyJSON)}},
		}},
	}
	for cluster, state := range compliance {
		p.Status.Status = append(p.Status.Status, &policiesv1.CompliancePerClusterStatus{
			ClusterName: cluster, ClusterNamespace: cluster, ComplianceState: state,
		})
	}
	return p
}

// childPolicy returns the replicated policy for parent in cluster's namespace.
func childPolicy(parentNs, parent, cluster string, mutate ...func(*policiesv1.Policy)) *policiesv1.Policy {
	p := &policiesv1.Policy{
		ObjectMeta: metav1.ObjectMeta{
			Name: parentNs + "." + parent, Namespace: cluster,
			Labels: map[string]string{utils.ChildPolicyLabel: parentNs + "." + parent},
		},
		Spec: policiesv1.PolicySpec{
			RemediationAction: policiesv1.Inform,
			PolicyTemplates:   []*policiesv1.PolicyTemplate{{ObjectDefinition: runtime.RawExtension{Raw: []byte(configPolicyJSON)}}},
		},
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

func policyNames(policies []*unstructured.Unstructured) []string {
	var names []string
	for _, p := range policies {
		names = append(names, p.GetName())
	}
	return names
}

func TestDoManagedPoliciesExist(t *testing.T) {
	nonCompliant := map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant, "spoke2": policiesv1.Compliant}
	compliant := map[string]policiesv1.ComplianceState{"spoke1": policiesv1.Compliant, "spoke2": policiesv1.Compliant}
	enforce := func(p *policiesv1.Policy) { p.Spec.RemediationAction = policiesv1.Enforce }
	hubTemplateError := func(p *policiesv1.Policy) {
		p.Spec.PolicyTemplates[0].ObjectDefinition.Raw = []byte(`{"spec":{"value":"{{hub fromConfigMap \"\" \"cm\" \"k\" hub}}"}}`)
	}

	tests := []struct {
		name              string
		policies          []string
		objs              []client.Object
		wantAllExist      bool
		wantForUpgrade    []ranv1alpha1.ManagedPolicyForUpgrade
		wantCompliant     []string
		wantPresent       []string
		wantMissing       []string
		wantInvalid       []string
		wantDuplicatedFor []string
	}{
		{
			name:     "non-compliant policy is upgraded, compliant one is recorded",
			policies: []string{"p1", "p2"},
			objs: []client.Object{
				rootPolicy("p1", nonCompliant), childPolicy(plcNamespace, "p1", "spoke1"),
				rootPolicy("p2", compliant), childPolicy(plcNamespace, "p2", "spoke1"),
			},
			wantAllExist:   true,
			wantForUpgrade: []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "p1", Namespace: plcNamespace}},
			wantCompliant:  []string{"p2"},
			wantPresent:    []string{"p1"},
		},
		{
			name:        "policy without a child on the selected clusters is missing",
			policies:    []string{"p1"},
			objs:        []client.Object{rootPolicy("p1", nonCompliant), childPolicy(plcNamespace, "p1", "spoke3")},
			wantMissing: []string{"p1"},
		},
		{
			name:        "child without a parent is missing",
			policies:    []string{"p1"},
			objs:        []client.Object{childPolicy(plcNamespace, "p1", "spoke1")},
			wantMissing: []string{"p1"},
		},
		{
			name:         "enforce policy is ignored",
			policies:     []string{"p1"},
			objs:         []client.Object{rootPolicy("p1", nonCompliant), childPolicy(plcNamespace, "p1", "spoke1", enforce)},
			wantAllExist: true,
		},
		{
			name:        "child with a hub template error is invalid",
			policies:    []string{"p1"},
			objs:        []client.Object{rootPolicy("p1", nonCompliant), childPolicy(plcNamespace, "p1", "spoke1", hubTemplateError)},
			wantInvalid: []string{"p1"},
		},
		{
			name:     "same policy name in two namespaces is a duplicate",
			policies: []string{"p1"},
			objs: []client.Object{
				childPolicy(plcNamespace, "p1", "spoke1"), childPolicy("ztp-site", "p1", "spoke2"),
			},
			wantDuplicatedFor: []string{"p1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := getFakeClientFromObjects(tc.objs...)
			require.NoError(t, err)
			cgu := newPolicyCGU(tc.policies...)

			allExist, info, err := newPolicyReconciler(t, c).doManagedPoliciesExist(context.Background(), cgu, []string{"spoke1", "spoke2"})
			require.NoError(t, err)
			assert.Equal(t, tc.wantAllExist, allExist)
			assert.Equal(t, tc.wantMissing, info.missingPolicies)
			assert.Equal(t, tc.wantInvalid, info.invalidPolicies)
			assert.Equal(t, tc.wantPresent, policyNames(info.presentPolicies))
			if tc.wantDuplicatedFor != nil {
				for _, name := range tc.wantDuplicatedFor {
					assert.Equal(t, []string{plcNamespace, "ztp-site"}, info.duplicatedPoliciesNs[name])
				}
				return
			}
			assert.Empty(t, info.duplicatedPoliciesNs)
			assert.Equal(t, tc.wantForUpgrade, cgu.Status.ManagedPoliciesForUpgrade)
			assert.Equal(t, tc.wantCompliant, cgu.Status.ManagedPoliciesCompliantBeforeUpgrade)
		})
	}
}

// doManagedPoliciesExist runs on every reconcile until the upgrade starts; the status lists must
// follow compliance changes in that time rather than keep entries from an earlier pass.
func TestDoManagedPoliciesExistRefreshesStatusLists(t *testing.T) {
	ctx := context.Background()
	cgu := newPolicyCGU("p1")
	child := childPolicy(plcNamespace, "p1", "spoke1")

	c, err := getFakeClientFromObjects(rootPolicy("p1", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant}), child)
	require.NoError(t, err)
	_, _, err = newPolicyReconciler(t, c).doManagedPoliciesExist(ctx, cgu, []string{"spoke1"})
	require.NoError(t, err)
	require.Equal(t, []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "p1", Namespace: plcNamespace}}, cgu.Status.ManagedPoliciesForUpgrade)
	require.Empty(t, cgu.Status.ManagedPoliciesCompliantBeforeUpgrade)

	// The policy becomes compliant before the CGU is enabled.
	c, err = getFakeClientFromObjects(rootPolicy("p1", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.Compliant}), child)
	require.NoError(t, err)
	_, _, err = newPolicyReconciler(t, c).doManagedPoliciesExist(ctx, cgu, []string{"spoke1"})
	require.NoError(t, err)
	assert.Empty(t, cgu.Status.ManagedPoliciesForUpgrade)
	assert.Equal(t, []string{"p1"}, cgu.Status.ManagedPoliciesCompliantBeforeUpgrade)

	// And drifts back to non-compliant.
	c, err = getFakeClientFromObjects(rootPolicy("p1", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant}), child)
	require.NoError(t, err)
	_, _, err = newPolicyReconciler(t, c).doManagedPoliciesExist(ctx, cgu, []string{"spoke1"})
	require.NoError(t, err)
	assert.Equal(t, []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "p1", Namespace: plcNamespace}}, cgu.Status.ManagedPoliciesForUpgrade)
	assert.Empty(t, cgu.Status.ManagedPoliciesCompliantBeforeUpgrade)
}

func TestDoManagedPoliciesExistErrors(t *testing.T) {
	ctx := context.Background()
	base, err := getFakeClientFromObjects(rootPolicy("p1", nil), childPolicy(plcNamespace, "p1", "spoke1"))
	require.NoError(t, err)

	listFails := interceptor.NewClient(base, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return errEtcd },
	})
	_, _, err = newPolicyReconciler(t, listFails).doManagedPoliciesExist(ctx, newPolicyCGU("p1"), []string{"spoke1"})
	assert.ErrorContains(t, err, "etcd unavailable")

	getFails := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return errEtcd
		},
	})
	_, _, err = newPolicyReconciler(t, getFails).doManagedPoliciesExist(ctx, newPolicyCGU("p1"), []string{"spoke1"})
	assert.ErrorContains(t, err, "etcd unavailable")
}

func TestReconcileResourcesCreatesPlacementAndBinding(t *testing.T) {
	ctx := context.Background()
	c, err := getFakeClientFromObjects()
	require.NoError(t, err)
	r := newPolicyReconciler(t, c)
	cgu := newPolicyCGU("p1")
	cgu.Status.ManagedPoliciesForUpgrade = []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "p1", Namespace: plcNamespace}}

	policy := &unstructured.Unstructured{}
	policy.SetName("p1")
	policy.SetNamespace(plcNamespace)

	require.NoError(t, r.reconcileResources(ctx, cgu, []*unstructured.Unstructured{policy}))
	// A second pass updates the existing objects instead of failing on create.
	require.NoError(t, r.reconcileResources(ctx, cgu, []*unstructured.Unstructured{policy}))

	safeName := "cgu-p1-placement-abcde"
	placement := &clusterv1beta1.Placement{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: safeName, Namespace: plcNamespace}, placement))
	assert.Equal(t, map[string]string{
		"app":                          "openshift-cluster-group-upgrades",
		plcTestCGULabel:                "cgu",
		plcTestCGUNsLabel:              "ztp-install",
		plcTestForPolicyLabel:          "p1",
		utils.ExcludeFromClusterBackup: "true",
	}, placement.Labels)
	names, err := utils.GetPlacementClusterNames(placement)
	require.NoError(t, err)
	assert.Empty(t, names)
	assert.Len(t, placement.Spec.Tolerations, 2)

	binding := &policiesv1.PlacementBinding{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: safeName, Namespace: plcNamespace}, binding))
	assert.Equal(t, safeName, binding.PlacementRef.Name)
	assert.Equal(t, "Placement", binding.PlacementRef.Kind)
	require.Len(t, binding.Subjects, 1)
	assert.Equal(t, "p1", binding.Subjects[0].Name)
	assert.Equal(t, policiesv1.Restricted, binding.SubFilter)
	assert.Equal(t, "enforce", string(binding.BindingOverrides.RemediationAction))

	assert.Equal(t, []string{safeName}, cgu.Status.Placements)
	assert.Equal(t, []string{safeName}, cgu.Status.PlacementBindings)
}

func TestEnsureBatchPlacementErrors(t *testing.T) {
	ctx := context.Background()
	policy := &unstructured.Unstructured{}
	policy.SetName("p1")
	policy.SetNamespace(plcNamespace)

	base, err := getFakeClientFromObjects()
	require.NoError(t, err)
	tests := []struct {
		name  string
		funcs interceptor.Funcs
	}{
		{"get", interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return errEtcd
		}}},
		{"create", interceptor.Funcs{Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return errEtcd
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newPolicyReconciler(t, interceptor.NewClient(base, tc.funcs))
			_, err := r.ensureBatchPlacement(ctx, newPolicyCGU("p1"), policy)
			assert.ErrorContains(t, err, "etcd unavailable")
			assert.ErrorContains(t, r.ensureBatchPlacementBinding(ctx, newPolicyCGU("p1"), "placement", policy), "etcd unavailable")
		})
	}
}

// newCGUPlacement returns a placement owned by cgu (as built by the controller) selecting clusters.
func newCGUPlacement(t *testing.T, cgu *ranv1alpha1.ClusterGroupUpgrade, name string, clusters ...string) *clusterv1beta1.Placement {
	t.Helper()
	p := newPolicyReconciler(t, nil).newBatchPlacement(cgu, "p1", plcNamespace, name, name)
	require.NoError(t, utils.SetPlacementClusterNames(p, clusters))
	return p
}

func placementClusters(t *testing.T, c client.Client, name string) []string {
	t.Helper()
	p := &clusterv1beta1.Placement{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: plcNamespace}, p))
	names, err := utils.GetPlacementClusterNames(p)
	require.NoError(t, err)
	return names
}

func TestUpdatePlacements(t *testing.T) {
	ctx := context.Background()
	zero, one := 0, 1
	newCGU := func() *ranv1alpha1.ClusterGroupUpgrade {
		cgu := newPolicyCGU("p1", "p2")
		cgu.Status.ManagedPoliciesForUpgrade = []ranv1alpha1.ManagedPolicyForUpgrade{
			{Name: "p1", Namespace: plcNamespace}, {Name: "p2", Namespace: plcNamespace},
		}
		cgu.Status.SafeResourceNames = map[string]string{
			plcNamespace + "/cgu-p1-placement": "p1-placement",
			plcNamespace + "/cgu-p2-placement": "p2-placement",
		}
		cgu.Status.Status.CurrentBatchRemediationProgress = map[string]*ranv1alpha1.ClusterRemediationProgress{
			"spoke1": {State: ranv1alpha1.InProgress, PolicyIndex: &zero},
			"spoke2": {State: ranv1alpha1.InProgress, PolicyIndex: &one},
			"spoke3": {State: ranv1alpha1.Completed},
		}
		return cgu
	}

	t.Run("adds in-progress clusters to their current policy's placement", func(t *testing.T) {
		c, err := getFakeClientFromObjects(newCGUPlacement(t, newCGU(), "p1-placement", "spoke0", "spoke1"), newCGUPlacement(t, newCGU(), "p2-placement"))
		require.NoError(t, err)
		require.NoError(t, newPolicyReconciler(t, c).updatePlacements(ctx, newCGU()))
		// spoke1 was already there and is not duplicated.
		assert.Equal(t, []string{"spoke0", "spoke1"}, placementClusters(t, c, "p1-placement"))
		assert.Equal(t, []string{"spoke2"}, placementClusters(t, c, "p2-placement"))
	})

	t.Run("unknown placement name is an error", func(t *testing.T) {
		c, err := getFakeClientFromObjects(newCGUPlacement(t, newCGU(), "p1-placement"))
		require.NoError(t, err)
		cgu := newCGU()
		delete(cgu.Status.SafeResourceNames, plcNamespace+"/cgu-p2-placement")
		assert.ErrorContains(t, newPolicyReconciler(t, c).updatePlacements(ctx, cgu), "placement object name cgu-p2-placement not found")
	})

	t.Run("missing placement object is an error", func(t *testing.T) {
		c, err := getFakeClientFromObjects(newCGUPlacement(t, newCGU(), "p2-placement"))
		require.NoError(t, err)
		err = newPolicyReconciler(t, c).updatePlacements(ctx, newCGU())
		assert.True(t, apierrors.IsNotFound(err), "got %v", err)
	})
}

func TestCleanupPlacements(t *testing.T) {
	ctx := context.Background()
	cgu := newPolicyCGU("p1", "p2")
	cgu.Status.ManagedPoliciesForUpgrade = []ranv1alpha1.ManagedPolicyForUpgrade{
		{Name: "p1", Namespace: plcNamespace}, {Name: "p2", Namespace: plcNamespace},
	}
	otherCGU := newPolicyCGU("p1")
	otherCGU.Name = "other-cgu"

	c, err := getFakeClientFromObjects(
		newCGUPlacement(t, cgu, "a", "spoke1"), newCGUPlacement(t, cgu, "b", "spoke1", "spoke2"),
		newCGUPlacement(t, otherCGU, "other", "spoke9"))
	require.NoError(t, err)
	require.NoError(t, newPolicyReconciler(t, c).cleanupPlacements(ctx, cgu))
	assert.Empty(t, placementClusters(t, c, "a"))
	assert.Empty(t, placementClusters(t, c, "b"))
	assert.Equal(t, []string{"spoke9"}, placementClusters(t, c, "other"))

	failing := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error { return errEtcd },
	})
	err = newPolicyReconciler(t, failing).cleanupPlacements(ctx, cgu)
	assert.ErrorContains(t, err, "errors cleaning up placements")
	assert.ErrorContains(t, err, "etcd unavailable")
}

func TestCheckDuplicateChildResources(t *testing.T) {
	ctx := context.Background()
	withDesired := func(name, desired string) *clusterv1beta1.Placement {
		p := newCGUPlacement(t, newPolicyCGU(), name)
		p.Annotations = map[string]string{utils.DesiredResourceName: desired}
		return p
	}

	c, err := getFakeClientFromObjects(withDesired("p1-old", "ns/p1"))
	require.NoError(t, err)
	r := newPolicyReconciler(t, c)
	safeNames := map[string]string{"ns/p1": "p1-new"}

	// The recorded safe name is kept.
	names, err := r.checkDuplicateChildResources(ctx, safeNames, nil, withDesired("p1-new", "ns/p1"))
	require.NoError(t, err)
	assert.Equal(t, []string{"p1-new"}, names)

	// A different object with the same desired name is stale and deleted.
	names, err = r.checkDuplicateChildResources(ctx, safeNames, names, withDesired("p1-old", "ns/p1"))
	require.NoError(t, err)
	assert.Equal(t, []string{"p1-new"}, names)
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "p1-old", Namespace: plcNamespace}, &clusterv1beta1.Placement{})))

	// Deleting it again (already gone) is not an error.
	_, err = r.checkDuplicateChildResources(ctx, safeNames, names, withDesired("p1-old", "ns/p1"))
	require.NoError(t, err)

	// A new desired name is recorded.
	names, err = r.checkDuplicateChildResources(ctx, safeNames, names, withDesired("p2-x", "ns/p2"))
	require.NoError(t, err)
	assert.Equal(t, []string{"p1-new", "p2-x"}, names)
	assert.Equal(t, "p2-x", safeNames["ns/p2"])

	// Objects without the annotation are listed as-is.
	names, err = r.checkDuplicateChildResources(ctx, safeNames, names, &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: "plain"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"p1-new", "p2-x", "plain"}, names)

	// Other delete errors are returned.
	failing := newPolicyReconciler(t, interceptor.NewClient(c, interceptor.Funcs{
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error { return errEtcd },
	}))
	_, err = failing.checkDuplicateChildResources(ctx, safeNames, names, withDesired("p1-older", "ns/p1"))
	assert.ErrorContains(t, err, "etcd unavailable")
}

func TestHandlePolicyTimeoutForCluster(t *testing.T) {
	zero, five := 0, 5
	cgu := newPolicyCGU("p1")
	cgu.Status.ManagedPoliciesForUpgrade = []ranv1alpha1.ManagedPolicyForUpgrade{{Name: "p1", Namespace: plcNamespace}}
	cgu.Status.Status.CurrentBatchRemediationProgress = map[string]*ranv1alpha1.ClusterRemediationProgress{
		"spoke1": {State: ranv1alpha1.InProgress, PolicyIndex: &zero},
		"spoke2": {State: ranv1alpha1.InProgress},
		"spoke3": {State: ranv1alpha1.InProgress, PolicyIndex: &five},
	}
	r := newPolicyReconciler(t, nil)

	state := ranv1alpha1.ClusterState{Name: "spoke1"}
	r.handlePolicyTimeoutForCluster(cgu, "spoke1", &state)
	assert.Equal(t, &ranv1alpha1.PolicyStatus{Name: "p1", Status: utils.ClusterStatusNonCompliant}, state.CurrentPolicy)

	for _, cluster := range []string{"spoke2", "spoke3"} { // missing index, out-of-range index
		state := ranv1alpha1.ClusterState{Name: cluster}
		r.handlePolicyTimeoutForCluster(cgu, cluster, &state)
		assert.Nil(t, state.CurrentPolicy, cluster)
	}
}

func TestPolicyCompliance(t *testing.T) {
	r := newPolicyReconciler(t, nil)
	toUnstructured := func(p *policiesv1.Policy) *unstructured.Unstructured {
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
		require.NoError(t, err)
		return &unstructured.Unstructured{Object: obj}
	}
	p1 := toUnstructured(rootPolicy("p1", map[string]policiesv1.ComplianceState{
		"spoke1": policiesv1.Compliant, "spoke2": policiesv1.NonCompliant, "spoke3": policiesv1.Pending,
	}))
	p2 := toUnstructured(rootPolicy("p2", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant}))
	noStatus := toUnstructured(rootPolicy("p3", nil))
	noClusterList := toUnstructured(rootPolicy("p4", nil))
	noClusterList.Object["status"] = map[string]interface{}{"compliant": "Compliant"}

	assert.Nil(t, r.getPolicyClusterStatus(noStatus))
	assert.Nil(t, r.getPolicyClusterStatus(noClusterList))

	assert.Equal(t, utils.ClusterStatusCompliant, r.getClusterComplianceWithPolicy("spoke1", p1))
	assert.Equal(t, utils.ClusterStatusNonCompliant, r.getClusterComplianceWithPolicy("spoke3", p1), "pending is non-compliant")
	assert.Equal(t, utils.ClusterNotMatchedWithPolicy, r.getClusterComplianceWithPolicy("spoke9", p1))
	assert.Equal(t, utils.ClusterStatusNonCompliant, r.getClusterComplianceWithPolicy("spoke1", noStatus))

	clusters := []string{"spoke1", "spoke2", "spoke3", "spoke9"}
	// Not-matched clusters count as non-compliant here.
	assert.Equal(t, []string{"spoke2", "spoke3", "spoke9"}, r.getClustersNonCompliantWithPolicy(clusters, p1))
	// Here only an explicit NonCompliant counts.
	assert.Equal(t, map[string]bool{"spoke1": true, "spoke2": true, "spoke3": true},
		r.getClustersNonCompliantWithManagedPolicies(clusters, []*unstructured.Unstructured{p1, p2}))
}

func TestUpdateDuplicatedManagedPoliciesInfo(t *testing.T) {
	info := &policiesInfo{}
	updateDuplicatedManagedPoliciesInfo(info, map[string][]string{
		"p1": {"ns-b", "ns-a"},
		"p2": {"ns-a"},
	})
	assert.Equal(t, map[string][]string{"p1": {"ns-a", "ns-b"}}, info.duplicatedPoliciesNs)
}

func TestRootPolicyHandlerOnUpdate(t *testing.T) {
	ctx := context.Background()
	finished := func(status metav1.ConditionStatus) func(*ranv1alpha1.ClusterGroupUpgrade) {
		return func(cgu *ranv1alpha1.ClusterGroupUpgrade) {
			cgu.Status.Conditions = []metav1.Condition{{Type: string(utils.ConditionTypes.Succeeded), Status: status}}
		}
	}
	cguWith := func(name string, policies []string, clusters []string, mutate ...func(*ranv1alpha1.ClusterGroupUpgrade)) *ranv1alpha1.ClusterGroupUpgrade {
		cgu := newPolicyCGU(policies...)
		cgu.Name = name
		cgu.Spec.Clusters = clusters
		for _, m := range mutate {
			m(cgu)
		}
		return cgu
	}

	tests := []struct {
		name     string
		old, new map[string]policiesv1.ComplianceState
		cgus     []client.Object
		want     []string
	}{
		{
			name: "compliance change enqueues the CGU that targets the cluster",
			old:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant, "spoke2": policiesv1.NonCompliant},
			new:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.Compliant, "spoke2": policiesv1.NonCompliant},
			cgus: []client.Object{
				cguWith("targets-spoke1", []string{"p1"}, []string{"spoke1"}),
				cguWith("targets-spoke2", []string{"p1"}, []string{"spoke2"}),
				cguWith("other-policy", []string{"p2"}, []string{"spoke1"}),
			},
			want: []string{"targets-spoke1"},
		},
		{
			name: "added and removed clusters count as changes",
			old:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant},
			new:  map[string]policiesv1.ComplianceState{"spoke2": policiesv1.NonCompliant},
			cgus: []client.Object{
				cguWith("targets-spoke1", []string{"p1"}, []string{"spoke1"}),
				cguWith("targets-spoke2", []string{"p1"}, []string{"spoke2"}),
			},
			want: []string{"targets-spoke1", "targets-spoke2"},
		},
		{
			name: "no compliance change enqueues nothing",
			old:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant},
			new:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant},
			cgus: []client.Object{cguWith("targets-spoke1", []string{"p1"}, []string{"spoke1"})},
		},
		{
			name: "finished CGUs are skipped whatever their outcome",
			old:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant},
			new:  map[string]policiesv1.ComplianceState{"spoke1": policiesv1.Compliant},
			cgus: []client.Object{
				cguWith("succeeded", []string{"p1"}, []string{"spoke1"}, finished(metav1.ConditionTrue)),
				cguWith("timed-out", []string{"p1"}, []string{"spoke1"}, finished(metav1.ConditionFalse)),
				cguWith("running", []string{"p1"}, []string{"spoke1"}),
			},
			want: []string{"running"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := getFakeClientFromObjects(tc.cgus...)
			require.NoError(t, err)
			q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer q.ShutDown()

			newPolicyReconciler(t, c).rootPolicyHandlerOnUpdate(ctx,
				event.UpdateEvent{ObjectOld: rootPolicy("p1", tc.old), ObjectNew: rootPolicy("p1", tc.new)}, q)

			assert.ElementsMatch(t, tc.want, drainQueue(q))
		})
	}
}

func TestRootPolicyHandlerOnUpdateErrors(t *testing.T) {
	ctx := context.Background()
	changed := event.UpdateEvent{
		ObjectOld: rootPolicy("p1", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.NonCompliant}),
		ObjectNew: rootPolicy("p1", map[string]policiesv1.ComplianceState{"spoke1": policiesv1.Compliant}),
	}

	t.Run("listing CGUs fails", func(t *testing.T) {
		base, err := getFakeClientFromObjects(newPolicyCGU("p1"))
		require.NoError(t, err)
		c := interceptor.NewClient(base, interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return errEtcd },
		})
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		newPolicyReconciler(t, c).rootPolicyHandlerOnUpdate(ctx, changed, q)
		assert.Empty(t, drainQueue(q))
	})

	t.Run("a CGU whose clusters cannot be resolved is skipped", func(t *testing.T) {
		noClusters := newPolicyCGU("p1")
		noClusters.Name = "no-clusters"
		noClusters.Spec.Clusters = nil
		ok := newPolicyCGU("p1")
		ok.Name = "ok"
		ok.Spec.Clusters = []string{"spoke1"}
		c, err := getFakeClientFromObjects(noClusters, ok)
		require.NoError(t, err)
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		newPolicyReconciler(t, c).rootPolicyHandlerOnUpdate(ctx, changed, q)
		assert.Equal(t, []string{"ok"}, drainQueue(q))
	})
}

// drainQueue returns the names of the requests queued so far.
func drainQueue(q workqueue.TypedRateLimitingInterface[reconcile.Request]) []string {
	var names []string
	for q.Len() > 0 {
		req, _ := q.Get()
		names = append(names, req.Name)
		q.Done(req)
	}
	return names
}
