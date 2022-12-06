package controllers

import (
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

func TestClusterGroupUpgradeReconciler_extractOpenshiftImagePlatformFromPolicies(t *testing.T) {
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
			got, err := r.extractOpenshiftImagePlatformFromPolicies(tt.args.policies)
			if !tt.wantErr(t, err, fmt.Sprintf("extractOpenshiftImagePlatformFromPolicies(%v)", tt.args.policies)) {
				return
			}
			assert.Equalf(t, tt.want, got, "extractOpenshiftImagePlatformFromPolicies(%v)", tt.args.policies)
		})
	}
}

func TestClusterGroupUpgradeReconciler_validateOpenshiftUpgradeVersion(t *testing.T) {
	type fields struct {
		Client   client.Client
		Log      logr.Logger
		Scheme   *runtime.Scheme
		Recorder record.EventRecorder
	}
	type args struct {
		clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade
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
			tt.wantErr(t, r.validateOpenshiftUpgradeVersion(tt.args.clusterGroupUpgrade, tt.args.policies), fmt.Sprintf("validateOpenshiftUpgradeVersion(%v, %v)", tt.args.clusterGroupUpgrade, tt.args.policies))
		})
	}
}
