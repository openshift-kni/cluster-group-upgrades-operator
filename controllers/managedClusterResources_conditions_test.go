package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const mcrCluster = "spoke1"

var errMCR = apierrors.NewInternalError(errors.New("etcd unavailable"))

func newMCRReconciler(t *testing.T, objs ...client.Object) (*ClusterGroupUpgradeReconciler, client.WithWatch) {
	t.Helper()
	c, err := getFakeClientFromObjects(objs...)
	require.NoError(t, err)
	return &ClusterGroupUpgradeReconciler{Client: c, Log: logr.Discard(), Scheme: testscheme}, c
}

func withClient(r *ClusterGroupUpgradeReconciler, c client.Client) *ClusterGroupUpgradeReconciler {
	return &ClusterGroupUpgradeReconciler{Client: c, Log: r.Log, Scheme: r.Scheme}
}

// jobView returns a precache job view; jobStatus is the Job's status as mirrored by the view.
func jobView(jobStatus map[string]interface{}) *unstructured.Unstructured {
	return precacheMCV("view-precache-job", mcrCluster, "", map[string]interface{}{
		"result": map[string]interface{}{"status": jobStatus},
	})
}

func failedJob(reason string) map[string]interface{} {
	return map[string]interface{}{"conditions": []interface{}{
		map[string]interface{}{"type": "Failed", "status": "True", "reason": reason},
	}}
}

func managedCluster(available metav1.ConditionStatus) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mcrCluster},
		Status: clusterv1.ManagedClusterStatus{Conditions: []metav1.Condition{
			{Type: clusterv1.ManagedClusterConditionAvailable, Status: available},
		}},
	}
}

func objectExists(t *testing.T, c client.Client, gvk schema.GroupVersionKind, name, cluster string) bool {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	err := c.Get(context.Background(), client.ObjectKey{Name: name, Namespace: cluster}, obj)
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err)
	return true
}

func managedClusterObject(gvk schema.GroupVersionKind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.SetNamespace(mcrCluster)
	return obj
}

func TestMCR_getJobStatus(t *testing.T) {
	tests := []struct {
		name    string
		view    *unstructured.Unstructured
		want    string
		wantErr bool
	}{
		{"active", jobView(map[string]interface{}{"active": int64(1)}), JobActive, false},
		{"succeeded", jobView(map[string]interface{}{"succeeded": int64(1)}), JobSucceeded, false},
		{"deadline exceeded", jobView(failedJob("DeadlineExceeded")), JobDeadline, false},
		{"backoff limit exceeded", jobView(failedJob("BackoffLimitExceeded")), JobBackoffLimitExceeded, false},
		{"failed for another reason", jobView(failedJob("PodFailurePolicy")), UnforeseenCondition, true},
		{"no outcome yet", jobView(map[string]interface{}{}), UnforeseenCondition, true},
		{"no job status in the view", precacheMCV("view-precache-job", mcrCluster, "", nil), UnforeseenCondition, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newMCRReconciler(t)
			got, err := r.getJobStatus(tc.view)
			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMCR_getPreparingConditions(t *testing.T) {
	noConditions := managedClusterObject(viewGroupVersionKind(), "view-precache-namespace")
	tests := []struct {
		name    string
		objs    []client.Object
		want    string
		wantErr bool
	}{
		{"no view", nil, NoNsView, false},
		{"namespace found", []client.Object{precacheMCV("view-precache-namespace", mcrCluster, "", nil)}, NsFoundOnSpoke, false},
		{"namespace not found", []client.Object{precacheMCV("view-precache-namespace", mcrCluster, `namespaces "openshift-talo-pre-cache" not found`, nil)}, NoNsFoundOnSpoke, false},
		{"view error", []client.Object{precacheMCV("view-precache-namespace", mcrCluster, "the server is down", nil)}, UnforeseenCondition, true},
		{"view without conditions", []client.Object{noConditions}, UnforeseenCondition, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newMCRReconciler(t, tc.objs...)
			got, err := r.getPreparingConditions(context.Background(), mcrCluster, "view-precache-namespace")
			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMCR_getActiveConditions(t *testing.T) {
	active := map[string]interface{}{"active": int64(1)}
	tests := []struct {
		name    string
		objs    []client.Object
		want    string
		wantErr string
	}{
		{name: "no job view", want: NoJobView},
		{
			name: "job not found on spoke",
			objs: []client.Object{precacheMCV("view-precache-job", mcrCluster, `jobs.batch "pre-cache" not found`, nil)},
			want: NoJobFoundOnSpoke,
		},
		{
			name: "view without conditions",
			objs: []client.Object{managedClusterObject(viewGroupVersionKind(), "view-precache-job")},
			want: UnforeseenCondition, wantErr: "no ManagedClusterView conditions found",
		},
		{
			name: "active job on an available cluster",
			objs: []client.Object{jobView(active), managedCluster(metav1.ConditionTrue)},
			want: JobActive,
		},
		{
			// The view can hold a stale "active" result when the cluster stops updating it.
			name: "active job on an unavailable cluster",
			objs: []client.Object{jobView(active), managedCluster(metav1.ConditionUnknown)},
			want: UnforeseenCondition, wantErr: "is unavailable",
		},
		{
			name: "active job on a missing cluster",
			objs: []client.Object{jobView(active)},
			want: UnforeseenCondition, wantErr: "not found",
		},
		{
			name: "succeeded job does not need the cluster",
			objs: []client.Object{jobView(map[string]interface{}{"succeeded": int64(1)})},
			want: JobSucceeded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newMCRReconciler(t, tc.objs...)
			got, err := r.getActiveConditions(context.Background(), mcrCluster, "view-precache-job")
			assert.Equal(t, tc.want, got)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMCR_getViewErrorsPropagate(t *testing.T) {
	r, c := newMCRReconciler(t)
	failing := withClient(r, interceptor.NewClient(c, interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return errMCR
		},
	}))
	ctx := context.Background()

	got, err := failing.getPreparingConditions(ctx, mcrCluster, "view-precache-namespace")
	assert.Equal(t, UnforeseenCondition, got)
	assert.ErrorContains(t, err, "etcd unavailable")

	got, err = failing.getActiveConditions(ctx, mcrCluster, "view-precache-job")
	assert.Equal(t, UnforeseenCondition, got)
	assert.ErrorContains(t, err, "etcd unavailable")

	_, err = failing.checkDependenciesViews(ctx, mcrCluster)
	assert.ErrorContains(t, err, "etcd unavailable")
	_, err = failing.checkDependencies(ctx, mcrCluster)
	assert.ErrorContains(t, err, "etcd unavailable")
}

func TestMCR_deleteManagedClusterResources(t *testing.T) {
	ctx := context.Background()
	r, c := newMCRReconciler(t,
		managedClusterObject(viewGroupVersionKind(), "view-precache-job"),
		managedClusterObject(actionGroupVersionKind(), "precache-job-create"),
		managedClusterObject(viewGroupVersionKind(), "unrelated-view"),
	)

	// Missing objects are ignored; only views and actions named in the list are deleted.
	require.NoError(t, r.deleteManagedClusterResources(ctx, mcrCluster, append(precacheAllViews, precacheMCAs...)))
	assert.False(t, objectExists(t, c, viewGroupVersionKind(), "view-precache-job", mcrCluster))
	assert.False(t, objectExists(t, c, actionGroupVersionKind(), "precache-job-create", mcrCluster))
	assert.True(t, objectExists(t, c, viewGroupVersionKind(), "unrelated-view", mcrCluster))

	require.NoError(t, r.deleteManagedClusterResource(ctx, "unrelated-view", mcrCluster, viewGroupVersionKind()))
	assert.False(t, objectExists(t, c, viewGroupVersionKind(), "unrelated-view", mcrCluster))

	failing := withClient(r, interceptor.NewClient(c, interceptor.Funcs{
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error { return errMCR },
	}))
	assert.ErrorContains(t, failing.deleteManagedClusterResources(ctx, mcrCluster, precacheAllViews), "etcd unavailable")
	assert.ErrorContains(t, failing.deleteDependenciesViews(ctx, mcrCluster), "etcd unavailable")
	t.Setenv("PRECACHE_IMG", "quay.io/talm/precache:latest")
	assert.ErrorContains(t, failing.deployPrecachingWorkload(ctx, precacheCGU(), mcrCluster, "view-precache-job", precacheCreateTemplates), "etcd unavailable")
}

func TestMCR_deleteDependenciesViews(t *testing.T) {
	var objs []client.Object
	for _, item := range precacheDependenciesViewTemplates {
		objs = append(objs, managedClusterObject(viewGroupVersionKind(), item.resourceName))
	}
	r, c := newMCRReconciler(t, objs...)
	require.NoError(t, r.deleteDependenciesViews(context.Background(), mcrCluster))
	for _, item := range precacheDependenciesViewTemplates {
		assert.False(t, objectExists(t, c, viewGroupVersionKind(), item.resourceName, mcrCluster), item.resourceName)
	}
	// Nothing left to delete is not an error.
	require.NoError(t, r.deleteDependenciesViews(context.Background(), mcrCluster))
}

func precacheCGU() *ranv1alpha1.ClusterGroupUpgrade {
	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{Name: "cgu", Namespace: "ztp-install"},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
			RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{Timeout: 240},
		},
		Status: ranv1alpha1.ClusterGroupUpgradeStatus{
			Precaching: &ranv1alpha1.PrecachingStatus{Clusters: []string{mcrCluster, "spoke2"}},
		},
	}
}

func TestMCR_getPrecacheJobTemplateData(t *testing.T) {
	r, _ := newMCRReconciler(t)

	t.Setenv("PRECACHE_IMG", "")
	_, err := r.getPrecacheJobTemplateData(precacheCGU(), mcrCluster)
	assert.ErrorContains(t, err, "PRECACHE_IMG")

	t.Setenv("PRECACHE_IMG", "quay.io/talm/precache:latest")
	data, err := r.getPrecacheJobTemplateData(precacheCGU(), mcrCluster)
	require.NoError(t, err)
	assert.Equal(t, mcrCluster, data.Cluster)
	assert.Equal(t, uint64(240*60), data.JobTimeout)
	assert.Equal(t, "quay.io/talm/precache:latest", data.WorkloadImage)
}

func TestMCR_deployPrecachingWorkload(t *testing.T) {
	ctx := context.Background()
	staleView := jobView(map[string]interface{}{"failed": int64(1)})

	t.Run("replaces the job view and creates the job action", func(t *testing.T) {
		t.Setenv("PRECACHE_IMG", "quay.io/talm/precache:latest")
		r, c := newMCRReconciler(t, staleView)
		require.NoError(t, r.deployPrecachingWorkload(ctx, precacheCGU(), mcrCluster, "view-precache-job", precacheCreateTemplates))

		assert.True(t, objectExists(t, c, actionGroupVersionKind(), "precache-job-create", mcrCluster))
		view := &unstructured.Unstructured{}
		view.SetGroupVersionKind(viewGroupVersionKind())
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "view-precache-job", Namespace: mcrCluster}, view))
		_, hasConditions, err := unstructured.NestedSlice(view.Object, "status", "conditions")
		require.NoError(t, err)
		assert.False(t, hasConditions, "the stale view should have been recreated")
	})

	t.Run("missing image is an error and creates nothing", func(t *testing.T) {
		t.Setenv("PRECACHE_IMG", "")
		r, c := newMCRReconciler(t)
		assert.ErrorContains(t, r.deployPrecachingWorkload(ctx, precacheCGU(), mcrCluster, "view-precache-job", precacheCreateTemplates), "PRECACHE_IMG")
		assert.False(t, objectExists(t, c, actionGroupVersionKind(), "precache-job-create", mcrCluster))
	})
}

func TestMCR_jobAndViewFinalCleanup(t *testing.T) {
	ctx := context.Background()
	hubObjects := func(cluster string) []client.Object {
		view := managedClusterObject(viewGroupVersionKind(), "view-precache-job")
		view.SetNamespace(cluster)
		action := managedClusterObject(actionGroupVersionKind(), "precache-job-create")
		action.SetNamespace(cluster)
		return []client.Object{view, action}
	}

	r, c := newMCRReconciler(t, append(hubObjects("spoke-failed"), hubObjects("spoke-done")...)...)
	cgu := precacheCGU()
	cgu.Status.Precaching.Status = map[string]string{"spoke-failed": PrecacheStateError, "spoke-done": PrecacheStateSucceeded}

	require.NoError(t, r.jobAndViewFinalCleanup(ctx, cgu))

	// Clusters that did not succeed have their hub objects removed and the spoke cleanup actions created.
	assert.False(t, objectExists(t, c, viewGroupVersionKind(), "view-precache-job", "spoke-failed"))
	assert.False(t, objectExists(t, c, actionGroupVersionKind(), "precache-job-create", "spoke-failed"))
	for _, item := range precacheDeleteTemplates {
		assert.True(t, objectExists(t, c, actionGroupVersionKind(), item.resourceName, "spoke-failed"), item.resourceName)
	}
	// Succeeded clusters are left alone.
	assert.True(t, objectExists(t, c, viewGroupVersionKind(), "view-precache-job", "spoke-done"))
	for _, item := range precacheDeleteTemplates {
		assert.False(t, objectExists(t, c, actionGroupVersionKind(), item.resourceName, "spoke-done"), item.resourceName)
	}

	// A second run does not fail on the already-created cleanup actions.
	require.NoError(t, r.jobAndViewFinalCleanup(ctx, cgu))

	// No precaching status: nothing to do.
	require.NoError(t, r.jobAndViewFinalCleanup(ctx, &ranv1alpha1.ClusterGroupUpgrade{}))
}
