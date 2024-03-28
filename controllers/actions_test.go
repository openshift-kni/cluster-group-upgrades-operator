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

func TestActions_manageClusterLabelsAndAnnotations(t *testing.T) {
	testcases := []struct {
		name                string
		currentLabels       map[string]string
		currentAnnotations  map[string]string
		inputLabels         map[string]any
		inputAnnotations    map[string]any
		expectedLabels      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name:          "add cluster labels only",
			currentLabels: map[string]string{"label1Key": "label1Value", "label2Key": "label2Value"},
			inputLabels: map[string]any{
				"add":    map[string]string{"label3Key": "label3Value", "label4Key": ""},
				"delete": map[string]string{},
				"remove": []string{},
			},
			expectedLabels: map[string]string{
				"label1Key": "label1Value",
				"label2Key": "label2Value",
				"label3Key": "label3Value",
				"label4Key": "",
			},
		},
		{
			name:               "add cluster annotations only",
			currentAnnotations: map[string]string{"ann1Key": "ann1Value"},
			inputAnnotations: map[string]any{
				"add":    map[string]string{"ann2Key": "ann2Value"},
				"remove": []string{},
			},
			expectedAnnotations: map[string]string{
				"ann1Key": "ann1Value",
				"ann2Key": "ann2Value",
			},
		},
		{
			name:               "override current cluster label and annotation",
			currentLabels:      map[string]string{"label1Key": "label1Value", "label2Key": "label2Value"},
			currentAnnotations: map[string]string{"ann1Key": "ann1Value"},
			inputLabels: map[string]any{
				"add":    map[string]string{"label2Key": "labelNewValue"},
				"delete": map[string]string{},
				"remove": []string{},
			},
			inputAnnotations: map[string]any{
				"add":    map[string]string{"ann1Key": "annNewValue"},
				"remove": []string{},
			},
			expectedLabels:      map[string]string{"label1Key": "label1Value", "label2Key": "labelNewValue"},
			expectedAnnotations: map[string]string{"ann1Key": "annNewValue"},
		},
		{
			name:          "delete cluster labels only",
			currentLabels: map[string]string{"label1Key": "label1Value", "label2Key": "label2Value"},
			inputLabels: map[string]any{
				"add":    map[string]string{},
				"delete": map[string]string{"label1Key": ""},
				"remove": []string{"label2Key"},
			},
			expectedLabels: nil,
		},
		{
			name:               "delete cluster annotations only",
			currentAnnotations: map[string]string{"ann1Key": "ann1Value"},
			inputAnnotations: map[string]any{
				"add":    map[string]string{},
				"remove": []string{"ann1Key"},
			},
			expectedAnnotations: nil,
		},
		{
			name: "cluster has no labels and annotations",
			inputLabels: map[string]any{
				"add":    map[string]string{"label2Key": "label2Value", "label3Key": "label3Value"},
				"delete": map[string]string{"label1Key": ""},
				"remove": []string{"label4Key"},
			},
			inputAnnotations: map[string]any{
				"add":    map[string]string{"ann1Key": "ann1Value"},
				"remove": []string{"ann2Key"},
			},
			expectedLabels:      map[string]string{"label2Key": "label2Value", "label3Key": "label3Value"},
			expectedAnnotations: map[string]string{"ann1Key": "ann1Value"},
		},
		{
			name:               "no cluster labels and annotations to handle - one",
			currentLabels:      map[string]string{"label1Key": "label1Value", "label2Key": "label2Value"},
			currentAnnotations: map[string]string{"ann1Key": "ann1Value"},
			inputLabels: map[string]any{
				"add":    map[string]string{},
				"delete": map[string]string{},
				"remove": []string{},
			},
			inputAnnotations: map[string]any{
				"add":    map[string]string{},
				"remove": []string{},
			},
			expectedLabels:      map[string]string{"label1Key": "label1Value", "label2Key": "label2Value"},
			expectedAnnotations: map[string]string{"ann1Key": "ann1Value"},
		},
		{
			name:                "no cluster labels and annotations to handle - two",
			currentLabels:       map[string]string{"label1Key": "label1Value"},
			currentAnnotations:  nil,
			expectedLabels:      map[string]string{"label1Key": "label1Value"},
			expectedAnnotations: nil,
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
					Name:        "testSpoke",
					Labels:      tc.currentLabels,
					Annotations: tc.currentAnnotations,
				},
			}
			if err := r.Create(context.Background(), cluster); err != nil {
				t.Errorf("Unexpected error when creating cluster: %v", err)
			}

			err := r.manageClusterLabelsAndAnnotations(context.Background(), cluster.Name, tc.inputLabels, tc.inputAnnotations)
			if err != nil {
				t.Errorf("Unexpected error when adding cluster labels: %v", err)
			}

			updatedCluster := &clusterv1.ManagedCluster{}
			if err := r.Get(context.Background(), types.NamespacedName{Name: "testSpoke"}, updatedCluster); err != nil {
				t.Errorf("Unexpected error %v", err)
			}
			assert.Equal(t, tc.expectedLabels, updatedCluster.Labels)
			assert.Equal(t, tc.expectedAnnotations, updatedCluster.Annotations)
		})
	}
}
