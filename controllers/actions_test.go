package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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

// TestDeleteResourcesRecoversNamespacesFromSafeResourceNames covers a CGU whose
// managed policies all became compliant while it waited to be enabled or for a
// blocking CGU: status.managedPoliciesForUpgrade is refreshed to empty, but the
// Placements and PlacementBindings created for the earlier entries still exist.
func TestDeleteResourcesRecoversNamespacesFromSafeResourceNames(t *testing.T) {
	ctx := context.Background()
	cgu := newPolicyCGU("p1")
	cgu.Status.Placements = []string{"cgu-p1-placement"}
	cgu.Status.PlacementBindings = []string{"cgu-p1-placement"}
	cgu.Status.SafeResourceNames = map[string]string{
		plcNamespace + "/cgu-p1-placement": "cgu-p1-placement",
		// Empty-namespace keys come from ManagedClusterViews and ManifestWorks and
		// must be skipped rather than deleting in every namespace.
		"/cgu-managedclusterview": "cgu-managedclusterview",
	}

	builder := newPolicyReconciler(t, nil)
	placement := builder.newBatchPlacement(cgu, "p1", plcNamespace, "cgu-p1-placement", "cgu-p1-placement")
	binding := builder.newBatchPlacementBinding(cgu, "p1", plcNamespace, "cgu-p1-placement", "cgu-p1-placement", "cgu-p1-placement")

	c, err := getFakeClientFromObjects(placement, binding)
	require.NoError(t, err)
	r := newPolicyReconciler(t, c)

	require.NoError(t, r.deleteResources(ctx, cgu))

	key := types.NamespacedName{Name: "cgu-p1-placement", Namespace: plcNamespace}
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, key, &clusterv1beta1.Placement{})), "placement should be deleted")

	gotBinding := &unstructured.Unstructured{}
	gotBinding.SetGroupVersionKind(binding.GroupVersionKind())
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, key, gotBinding)), "placement binding should be deleted")

	assert.Nil(t, cgu.Status.SafeResourceNames)
	assert.Nil(t, cgu.Status.Placements)
	assert.Nil(t, cgu.Status.PlacementBindings)
}
