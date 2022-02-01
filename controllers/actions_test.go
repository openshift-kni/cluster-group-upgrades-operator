package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestActions_addClusterLabels(t *testing.T) {
	testcases := []struct {
		name           string
		currentLabels  map[string]string
		addLabels      map[string]string
		expectedLabels map[string]string
	}{
		{
			name: "add cluster labels",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
			addLabels: map[string]string{
				"label3Key": "label3Value",
				"label4Key": "",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
				"label3Key": "label3Value",
				"label4Key": "",
			},
		},
		{
			name:          "cluster has no labels",
			currentLabels: map[string]string{},
			addLabels: map[string]string{
				"label2Key": "label2Value",
				"label3Key": "label3Value",
			},
			expectedLabels: map[string]string{
				"label2Key": "label2Value",
				"label3Key": "label3Value",
			},
		},
		{
			name: "override current cluster label",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
			addLabels: map[string]string{
				"label2Key": "labelNewValue",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "labelNewValue",
			},
		},
		{
			name: "no cluster labels to add",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
			addLabels: map[string]string{},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: v1.ObjectMeta{
					Name:   "testSpoke",
					Labels: tc.currentLabels,
				},
			}
			if err := r.Create(context.TODO(), cluster); err != nil {
				t.Errorf("Unexpected error when creating cluster: %v", err)
			}

			err := r.addClusterLabels(context.TODO(), cluster, tc.addLabels)
			if err != nil {
				t.Errorf("Unexpected error when adding cluster labels: %v", err)
			}

			updatedCluster := &clusterv1.ManagedCluster{}
			if err := r.Get(context.TODO(), types.NamespacedName{Name: "testSpoke"}, updatedCluster); err != nil {
				t.Errorf("Unexpected error %v", err)
			}
			assert.Equal(t, tc.expectedLabels, updatedCluster.Labels)
		})
	}
}

func TestActions_deleteClusterLabels(t *testing.T) {
	testcases := []struct {
		name           string
		currentLabels  map[string]string
		deleteLabels   map[string]string
		expectedLabels map[string]string
	}{
		{
			name: "delete cluster labels",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
				"label3Key": "label3Value",
				"label4Key": "",
			},
			deleteLabels: map[string]string{
				"label3Key": "label3Value",
				"label4Key": "",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
		},
		{
			name:          "cluster has no labels",
			currentLabels: map[string]string{},
			deleteLabels: map[string]string{
				"label2Key": "label2Value",
				"label3Key": "label3Value",
			},
			expectedLabels: nil,
		},
		{
			name: "delete label has different value",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
			deleteLabels: map[string]string{
				"label2Key": "labelNewValue",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
		},
		{
			name: "delete label(key:\"\") doesn't exist",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label3Key": "",
			},
			deleteLabels: map[string]string{
				"label2Key": "",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label3Key": "",
			},
		},
		{
			name: "delete label(key:value) doesn't exist",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label3Key": "label3Value",
			},
			deleteLabels: map[string]string{
				"label2Key": "label2Value",
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label3Key": "label3Value",
			},
		},
		{
			name: "no cluster labels to delete",
			currentLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
			deleteLabels: map[string]string{},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: v1.ObjectMeta{
					Name:   "testSpoke",
					Labels: tc.currentLabels,
				},
			}
			if err := r.Create(context.TODO(), cluster); err != nil {
				t.Errorf("Unexpected error when creating cluster: %v", err)
			}

			err := r.deleteClusterLabels(context.TODO(), cluster, tc.deleteLabels)
			if err != nil {
				t.Errorf("Unexpected error when deleting cluster labels: %v", err)
			}

			updatedCluster := &clusterv1.ManagedCluster{}
			if err := r.Get(context.TODO(), types.NamespacedName{Name: "testSpoke"}, updatedCluster); err != nil {
				t.Errorf("Unexpected error %v", err)
			}
			assert.Equal(t, tc.expectedLabels, updatedCluster.Labels)
		})
	}
}
