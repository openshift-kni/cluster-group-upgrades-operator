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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestClusterGroupUpgradeReconciler_checkAllPrecachingDone(t *testing.T) {
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
			r.checkAllPrecachingDone(tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_handleActive(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx     context.Context
		cluster string
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
			got, err := r.handleActive(tt.args.ctx, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("handleActive(%v, %v)", tt.args.ctx, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "handleActive(%v, %v)", tt.args.ctx, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_handleNotStarted(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx     context.Context
		cluster string
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
			got, err := r.handleNotStarted(tt.args.ctx, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("handleNotStarted(%v, %v)", tt.args.ctx, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "handleNotStarted(%v, %v)", tt.args.ctx, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_handlePreparing(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx     context.Context
		cluster string
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
			got, err := r.handlePreparing(tt.args.ctx, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("handlePreparing(%v, %v)", tt.args.ctx, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "handlePreparing(%v, %v)", tt.args.ctx, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_handleStarting(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		cluster             string
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
			got, err := r.handleStarting(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("handleStarting(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "handleStarting(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_precachingFsm(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
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
			tt.wantErr(t, r.precachingFsm(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.policies), fmt.Sprintf("precachingFsm(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters, tt.args.policies))
		})
	}
}

func TestClusterGroupUpgradeReconciler_setPrecachingStartedCondition(t *testing.T) {
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
			r.setPrecachingStartedCondition(tt.args.clusterGroupUpgrade)
		})
	}
}
