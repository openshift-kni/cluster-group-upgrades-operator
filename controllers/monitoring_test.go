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

func TestClusterGroupUpgradeReconciler_getMonitoredObjects(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		managedPolicy *unstructured.Unstructured
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []ConfigurationObject
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
			got, err := r.getMonitoredObjects(tt.args.managedPolicy)
			if !tt.wantErr(t, err, fmt.Sprintf("getMonitoredObjects(%v)", tt.args.managedPolicy)) {
				return
			}
			assert.Equalf(t, tt.want, got, "getMonitoredObjects(%v)", tt.args.managedPolicy)
		})
	}
}

func TestClusterGroupUpgradeReconciler_processManagedPolicyForMonitoredObjects(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade       *ranv1alpha1.ClusterGroupUpgrade
		managedPoliciesForUpgrade []*unstructured.Unstructured
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
			tt.wantErr(t, r.processManagedPolicyForMonitoredObjects(tt.args.clusterGroupUpgrade, tt.args.managedPoliciesForUpgrade), fmt.Sprintf("processManagedPolicyForMonitoredObjects(%v, %v)", tt.args.clusterGroupUpgrade, tt.args.managedPoliciesForUpgrade))
		})
	}
}

func TestClusterGroupUpgradeReconciler_processMonitoredObject(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		ctx                 context.Context
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
		object              ConfigurationObject
		clusterName         string
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
			got, err := r.processMonitoredObject(tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.object, tt.args.clusterName)
			if !tt.wantErr(t, err, fmt.Sprintf("processMonitoredObject(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.object, tt.args.clusterName)) {
				return
			}
			assert.Equalf(t, tt.want, got, "processMonitoredObject(%v, %v, %v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade, tt.args.object, tt.args.clusterName)
		})
	}
}

func TestClusterGroupUpgradeReconciler_processMonitoredObjects(t *testing.T) {
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
			got, err := r.processMonitoredObjects(tt.args.ctx, tt.args.clusterGroupUpgrade)
			if !tt.wantErr(t, err, fmt.Sprintf("processMonitoredObjects(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)) {
				return
			}
			assert.Equalf(t, tt.want, got, "processMonitoredObjects(%v, %v)", tt.args.ctx, tt.args.clusterGroupUpgrade)
		})
	}
}

func Test_isMonitoredObjectType(t *testing.T) {
	type args struct {
		kind interface{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, isMonitoredObjectType(tt.args.kind), "isMonitoredObjectType(%v)", tt.args.kind)
		})
	}
}
