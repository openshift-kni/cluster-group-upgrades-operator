package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestClusterGroupUpgradeReconciler_backupActive(t *testing.T) {
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
			got, err := r.backupActive(tt.args.ctx, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("backupActive(%v, %v)", tt.args.ctx, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "backupActive(%v, %v)", tt.args.ctx, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_backupPreparing(t *testing.T) {
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
			got, err := r.backupPreparing(tt.args.ctx, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("backupPreparing(%v, %v)", tt.args.ctx, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "backupPreparing(%v, %v)", tt.args.ctx, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_backupStarting(t *testing.T) {
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
			got, err := r.backupStarting(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
			if !tt.wantErr(t, err, fmt.Sprintf("backupStarting(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)) {
				return
			}
			assert.Equalf(t, tt.want, got, "backupStarting(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.cluster)
		})
	}
}

func TestClusterGroupUpgradeReconciler_checkAllBackupDone(t *testing.T) {
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
			r.checkAllBackupDone(tt.args.clusterGroupUpgrade)
		})
	}
}

func TestClusterGroupUpgradeReconciler_reconcileBackup(t *testing.T) {
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
			tt.wantErr(t, r.reconcileBackup(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters), fmt.Sprintf("reconcileBackup(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters))
		})
	}
}

func TestClusterGroupUpgradeReconciler_triggerBackup(t *testing.T) {
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
			tt.wantErr(t, r.triggerBackup(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters), fmt.Sprintf("triggerBackup(%v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.clusters))
		})
	}
}
