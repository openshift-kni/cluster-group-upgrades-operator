package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestClusterGroupUpgradeReconciler_checkPreCacheSpecConsistency(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		spec v1alpha1.PrecachingSpec
	}
	tests := []struct {
		name           string
		fields         fields
		args           args
		wantConsistent bool
		wantMessage    string
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
			gotConsistent, gotMessage := r.checkPreCacheSpecConsistency(tt.args.spec)
			assert.Equalf(t, tt.wantConsistent, gotConsistent, "checkPreCacheSpecConsistency(%v)", tt.args.spec)
			assert.Equalf(t, tt.wantMessage, gotMessage, "checkPreCacheSpecConsistency(%v)", tt.args.spec)
		})
	}
}

func TestClusterGroupUpgradeReconciler_deployDependencies(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *v1alpha1.ClusterGroupUpgrade
		cluster             string
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
			got, err := r.deployDependencies(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("deployDependencies(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "deployDependencies(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_extractPrecachingSpecFromPolicies(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		policies []*unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    v1alpha1.PrecachingSpec
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
			got, err := r.extractPrecachingSpecFromPolicies(tt.args.policies)
			if !tt.wantErr(t, err, fmt.Sprintf("extractPrecachingSpecFromPolicies(%v)", tt.args.policies)) {
				return
			}
			assert.Equalf(t, tt.want, got, "extractPrecachingSpecFromPolicies(%v)", tt.args.policies)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getImageForVersionFromUpdateGraph(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		upstream string
		channel  string
		version  string
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
			got, err := r.getImageForVersionFromUpdateGraph(tt.args.upstream, tt.args.channel, tt.args.version)
			if !tt.wantErr(t, err, fmt.Sprintf("getImageForVersionFromUpdateGraph(%v, %v, %v)", tt.args.upstream, tt.args.channel, tt.args.version)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getImageForVersionFromUpdateGraph(%v, %v, %v)", tt.args.upstream, tt.args.channel, tt.args.version)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPrecacheSpecTemplateData(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *v1alpha1.ClusterGroupUpgrade
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *templateData
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
			assert.Equalf(t, tt.want, r.getPrecacheSpecTemplateData(tt.args.clusterGroupUpgrade), "getPrecacheSpecTemplateData(%v)", tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_getPrecacheimagePullSpec(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *v1alpha1.ClusterGroupUpgrade
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
			got, err := r.getPrecacheimagePullSpec(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("getPrecacheimagePullSpec(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getPrecacheimagePullSpec(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_includeSoftwareSpecOverrides(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *v1alpha1.ClusterGroupUpgrade
		spec                *v1alpha1.PrecachingSpec
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    v1alpha1.PrecachingSpec
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
			got, err := r.includeSoftwareSpecOverrides(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.spec)
			if !tt.wantErr(t, err, fmt.Sprintf("includeSoftwareSpecOverrides(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.spec)) {
				return
			}
			assert.Equalf(t, tt.want, got, "includeSoftwareSpecOverrides(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.spec)
		})
	}
}

func TestClusterGroupUpgradeReconciler_reconcilePrecaching(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *v1alpha1.ClusterGroupUpgrade
		clusters            []string
		policies            []*unstructured.Unstructured
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
			tt.wantErr(t, r.reconcilePrecaching(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.policies), fmt.Sprintf("reconcilePrecaching(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.policies))
		})
	}
}

func TestClusterGroupUpgradeReconciler_stripPolicy(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		policyObject map[string]interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []map[string]interface{}
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
			got, err := r.stripPolicy(tt.args.policyObject)
			if !tt.wantErr(t, err, fmt.Sprintf("stripPolicy(%v)", tt.args.policyObject)) {
				return
			}
			assert.Equalf(t, tt.want, got, "stripPolicy(%v)", tt.args.policyObject)
		})
	}
}
