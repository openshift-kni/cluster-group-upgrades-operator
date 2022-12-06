package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

func TestClusterGroupUpgradeReconciler_Reconcile(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx context.Context
		req controllerruntime.Request
	}
	tests := []struct {
		name              string
		fields            fields
		args              args
		wantNextReconcile controllerruntime.Result
		wantErr           assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			gotNextReconcile, err := r.Reconcile(tt.args.ctx, tt.args.req)
			if !tt.wantErr(t, err, fmt.Sprintf("Reconcile(%v, %v)", tt.args.ctx, tt.args.req)) {
				return
			}
			assert.Equalf(t, tt.wantNextReconcile, gotNextReconcile, "Reconcile(%v, %v)", tt.args.ctx, tt.args.req)
		})
	}
}

func TestClusterGroupUpgradeReconciler_SetupWithManager(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		mgr controllerruntime.Manager
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.SetupWithManager(tt.args.mgr), fmt.Sprintf("SetupWithManager(%v)", tt.args.mgr))
		})
	}
}

func TestClusterGroupUpgradeReconciler_addClustersStatusOnCompleteBatch(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			r.addClustersStatusOnCompleteBatch(tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_addClustersStatusOnTimeout(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			r.addClustersStatusOnTimeout(tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_blockingCRsNotCompleted(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		want1   []string
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, got1, err := r.blockingCRsNotCompleted(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("blockingCRsNotCompleted(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "blockingCRsNotCompleted(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
			assert.Equalf(t, tt.want1, got1, "blockingCRsNotCompleted(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_buildRemediationPlan(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		clusters            []string
		managedPolicies     []*unstructured.Unstructured
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			r.buildRemediationPlan(tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.managedPolicies)
		})
	}
}

func TestClusterGroupUpgradeReconciler_checkDuplicateChildResources(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                context.Context
		safeNameMap        map[string]string
		childResourceNames []string
		newResource        *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.checkDuplicateChildResources(tt.args.ctx, tt.args.safeNameMap, tt.args.childResourceNames, tt.args.newResource)
			if !tt.wantErr(t, err, fmt.Sprintf("checkDuplicateChildResources(%v, %v, %v, %v)", tt.args.ctx, tt.args.safeNameMap, tt.args.childResourceNames, tt.args.newResource)) {
				return
			}
			assert.Equalf(t, tt.want, got, "checkDuplicateChildResources(%v, %v, %v, %v)", tt.args.ctx, tt.args.safeNameMap, tt.args.childResourceNames, tt.args.newResource)
		})
	}
}

func TestClusterGroupUpgradeReconciler_cleanupPlacementRules(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.cleanupPlacementRules(tt.args.ctx, tt.args.clusterGroupUpgrade), fmt.Sprintf("cleanupPlacementRules(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade))
		})
	}
}

func TestClusterGroupUpgradeReconciler_copyManagedInformPolicy(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		managedPolicy       *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.copyManagedInformPolicy(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPolicy)
			if !tt.wantErr(t, err, fmt.Sprintf("copyManagedInformPolicy(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPolicy)) {
				return
			}
			assert.Equalf(t, tt.want, got, "copyManagedInformPolicy(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPolicy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_createNewPolicyFromStructure(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		policy              *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.createNewPolicyFromStructure(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policy), fmt.Sprintf("createNewPolicyFromStructure(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policy))
		})
	}
}

func TestClusterGroupUpgradeReconciler_doManagedPoliciesExist(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                        context.Context
		clusterGroupUpgrade        *ranv1alpha1.ClusterGroupUpgrade
		clusters                   []string
		filterNonCompliantPolicies bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		want1   policiesInfo
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, got1, err := r.doManagedPoliciesExist(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.filterNonCompliantPolicies)
			if !tt.wantErr(t, err, fmt.Sprintf("doManagedPoliciesExist(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.filterNonCompliantPolicies)) {
				return
			}
			assert.Equalf(t, tt.want, got, "doManagedPoliciesExist(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.filterNonCompliantPolicies)
			assert.Equalf(t, tt.want1, got1, "doManagedPoliciesExist(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.filterNonCompliantPolicies)
		})
	}
}

func TestClusterGroupUpgradeReconciler_ensureBatchPlacementBinding(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		policyName          string
		placementRuleName   string
		managedPolicy       *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.ensureBatchPlacementBinding(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.managedPolicy), fmt.Sprintf("ensureBatchPlacementBinding(%v, %v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.managedPolicy))
		})
	}
}

func TestClusterGroupUpgradeReconciler_ensureBatchPlacementRule(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		policyName          string
		managedPolicy       *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.ensureBatchPlacementRule(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.managedPolicy)
			if !tt.wantErr(t, err, fmt.Sprintf("ensureBatchPlacementRule(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.managedPolicy)) {
				return
			}
			assert.Equalf(t, tt.want, got, "ensureBatchPlacementRule(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.managedPolicy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_filterFailedBackupClusters(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		clusters            []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.filterFailedBackupClusters(tt.args.clusterGroupUpgrade, tt.args.clusters), "filterFailedBackupClusters(%v, %v)", tt.args.clusterGroupUpgrade, tt.args.clusters)
		})
	}
}

func TestClusterGroupUpgradeReconciler_filterFailedPrecachingClusters(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		clusters            []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.filterFailedPrecachingClusters(tt.args.clusterGroupUpgrade, tt.args.clusters), "filterFailedPrecachingClusters(%v, %v)", tt.args.clusterGroupUpgrade, tt.args.clusters)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getAllClustersForUpgrade(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getAllClustersForUpgrade(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("getAllClustersForUpgrade(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getAllClustersForUpgrade(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getClusterComplianceWithPolicy(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterName string
		policy      *unstructured.Unstructured
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.getClusterComplianceWithPolicy(tt.args.clusterName, tt.args.policy), "getClusterComplianceWithPolicy(%v, %v)", tt.args.clusterName, tt.args.policy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getClustersListFromRemediationPlan(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.getClustersListFromRemediationPlan(tt.args.clusterGroupUpgrade), "getClustersListFromRemediationPlan(%v)", tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getClustersNonCompliantWithManagedPolicies(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusters        []string
		managedPolicies []*unstructured.Unstructured
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   map[string]bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.getClustersNonCompliantWithManagedPolicies(tt.args.clusters, tt.args.managedPolicies), "getClustersNonCompliantWithManagedPolicies(%v, %v)", tt.args.clusters, tt.args.managedPolicies)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getClustersNonCompliantWithPolicy(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusters []string
		policy   *unstructured.Unstructured
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.getClustersNonCompliantWithPolicy(tt.args.clusters, tt.args.policy), "getClustersNonCompliantWithPolicy(%v, %v)", tt.args.clusters, tt.args.policy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getCopiedPolicies(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *unstructured.UnstructuredList
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getCopiedPolicies(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("getCopiedPolicies(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getCopiedPolicies(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getNextNonCompliantPolicyForCluster(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		clusterName         string
		startIndex          int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    int
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getNextNonCompliantPolicyForCluster(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusterName, tt.args.startIndex)
			if !tt.wantErr(t, err, fmt.Sprintf("getNextNonCompliantPolicyForCluster(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusterName, tt.args.startIndex)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getNextNonCompliantPolicyForCluster(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusterName, tt.args.startIndex)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getNextRemediationPoliciesForBatch(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getNextRemediationPoliciesForBatch(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("getNextRemediationPoliciesForBatch(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getNextRemediationPoliciesForBatch(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPlacementBindings(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *unstructured.UnstructuredList
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getPlacementBindings(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("getPlacementBindings(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getPlacementBindings(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPlacementRules(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		policyName          *string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *unstructured.UnstructuredList
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getPlacementRules(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName)
			if !tt.wantErr(t, err, fmt.Sprintf("getPlacementRules(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getPlacementRules(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policyName)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPolicyByName(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx        context.Context
		policyName string
		namespace  string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *unstructured.Unstructured
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.getPolicyByName(tt.args.ctx, tt.args.policyName, tt.args.namespace)
			if !tt.wantErr(t, err, fmt.Sprintf("getPolicyByName(%v, %v, %v)", tt.args.ctx, tt.args.policyName, tt.args.namespace)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getPolicyByName(%v, %v, %v)", tt.args.ctx, tt.args.policyName, tt.args.namespace)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPolicyClusterStatus(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		policy *unstructured.Unstructured
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []interface{}
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.getPolicyClusterStatus(tt.args.policy), "getPolicyClusterStatus(%v)", tt.args.policy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_handleCguFinalizer(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    int
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.handleCguFinalizer(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("handleCguFinalizer(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "handleCguFinalizer(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_initializeRemediationPolicyForBatch(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			r.initializeRemediationPolicyForBatch(tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_isUpgradeComplete(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.isUpgradeComplete(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("isUpgradeComplete(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "isUpgradeComplete(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_newBatchPlacementBinding(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade  *ranv1alpha1.ClusterGroupUpgrade
		policyName           string
		placementRuleName    string
		placementBindingName string
		desiredName          string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *unstructured.Unstructured
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.newBatchPlacementBinding(tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.placementBindingName, tt.args.desiredName), "newBatchPlacementBinding(%v, %v, %v, %v, %v)", tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.placementBindingName, tt.args.desiredName)
		})
	}
}

func TestClusterGroupUpgradeReconciler_newBatchPlacementRule(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		policyName          string
		placementRuleName   string
		desiredName         string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *unstructured.Unstructured
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			assert.Equalf(t, tt.want, r.newBatchPlacementRule(tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.desiredName), "newBatchPlacementRule(%v, %v, %v, %v)", tt.args.clusterGroupUpgrade, tt.args.policyName, tt.args.placementRuleName, tt.args.desiredName)
		})
	}
}

func TestClusterGroupUpgradeReconciler_reconcileResources(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                    context.Context
		clusterGroupUpgrade    *ranv1alpha1.ClusterGroupUpgrade
		managedPoliciesPresent []*unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, err := r.reconcileResources(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPoliciesPresent)
			if !tt.wantErr(t, err, fmt.Sprintf("reconcileResources(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPoliciesPresent)) {
				return
			}
			assert.Equalf(t, tt.want, got, "reconcileResources(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.managedPoliciesPresent)
		})
	}
}

func TestClusterGroupUpgradeReconciler_remediateCurrentBatch(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		nextReconcile       *controllerruntime.Result
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.remediateCurrentBatch(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.nextReconcile), fmt.Sprintf("remediateCurrentBatch(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.nextReconcile))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updateChildResourceNamesInStatus(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updateChildResourceNamesInStatus(tt.args.ctx, tt.args.clusterGroupUpgrade), fmt.Sprintf("updateChildResourceNamesInStatus(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updateConfigurationPolicyForCopiedPolicy(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                    context.Context
		clusterGroupUpgrade    *ranv1alpha1.ClusterGroupUpgrade
		policy                 *unstructured.Unstructured
		managedPolicyName      string
		managedPolicyNamespace string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updateConfigurationPolicyForCopiedPolicy(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policy, tt.args.managedPolicyName, tt.args.managedPolicyNamespace), fmt.Sprintf("updateConfigurationPolicyForCopiedPolicy(%v, %v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.policy, tt.args.managedPolicyName, tt.args.managedPolicyNamespace))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updateConfigurationPolicyHubTemplate(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                    context.Context
		objectDef              interface{}
		cguNamespace           string
		managedPolicyName      string
		managedPolicyNamespace string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updateConfigurationPolicyHubTemplate(tt.args.ctx, tt.args.objectDef, tt.args.cguNamespace, tt.args.managedPolicyName, tt.args.managedPolicyNamespace), fmt.Sprintf("updateConfigurationPolicyHubTemplate(%v, %v, %v, %v, %v)", tt.args.ctx, tt.args.objectDef, tt.args.cguNamespace, tt.args.managedPolicyName, tt.args.managedPolicyNamespace))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updateConfigurationPolicyName(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		metadata            interface{}
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			r.updateConfigurationPolicyName(tt.args.clusterGroupUpgrade, tt.args.metadata)
		})
	}
}

func TestClusterGroupUpgradeReconciler_updatePlacementRuleWithClusters(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		clusterNames        []string
		prName              string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updatePlacementRuleWithClusters(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusterNames, tt.args.prName), fmt.Sprintf("updatePlacementRuleWithClusters(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusterNames, tt.args.prName))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updatePlacementRules(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updatePlacementRules(tt.args.ctx, tt.args.clusterGroupUpgrade), fmt.Sprintf("updatePlacementRules(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade))
		})
	}
}

func TestClusterGroupUpgradeReconciler_updateStatus(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			tt.wantErr(t, r.updateStatus(tt.args.ctx, tt.args.clusterGroupUpgrade), fmt.Sprintf("updateStatus(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade))
		})
	}
}

func TestClusterGroupUpgradeReconciler_validateCR(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		want1   bool
		wantErr assert.ErrorAssertionFunc
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client:   tt.fields.Client,
				Log:      tt.fields.Log,
				Scheme:   tt.fields.Scheme,
				Recorder: tt.fields.Recorder,
			}
			got, got1, err := r.validateCR(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("validateCR(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "validateCR(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
			assert.Equalf(t, tt.want1, got1, "validateCR(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func Test_doNotRequeue(t *testing.T) {
	tests := []struct {
		name string
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, doNotRequeue(), "doNotRequeue()")
		})
	}
}

func Test_requeueImmediately(t *testing.T) {
	tests := []struct {
		name string
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, requeueImmediately(), "requeueImmediately()")
		})
	}
}

func Test_requeueWithCustomInterval(t *testing.T) {
	type args struct {
		interval time.Duration
	}
	tests := []struct {
		name string
		args args
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, requeueWithCustomInterval(tt.args.interval), "requeueWithCustomInterval(%v)", tt.args.interval)
		})
	}
}

func Test_requeueWithLongInterval(t *testing.T) {
	tests := []struct {
		name string
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, requeueWithLongInterval(), "requeueWithLongInterval()")
		})
	}
}

func Test_requeueWithMediumInterval(t *testing.T) {
	tests := []struct {
		name string
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, requeueWithMediumInterval(), "requeueWithMediumInterval()")
		})
	}
}

func Test_requeueWithShortInterval(t *testing.T) {
	tests := []struct {
		name string
		want controllerruntime.Result
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, requeueWithShortInterval(), "requeueWithShortInterval()")
		})
	}
}
