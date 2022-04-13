package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOverrides_getOverrides(t *testing.T) {
	testcases := []struct {
		name     string
		objectNs string
		readNs   string
		wrData   map[string]string
		rdData   map[string]string
	}{
		{
			name:     "Overrides exist",
			objectNs: "test",
			readNs:   "test",
			wrData: map[string]string{
				"precache.image":                "test-image:test-tag",
				"platform.image":                "test-platform-image:test-tag",
				"operators.indexes":             "registry.example.com:5000/test-index:v0.0",
				"operators.packagesAndChannels": "local-storage-operator: stable\nperformance-addon-operator: 4.9\nptp-operator: stable\nsriov-network-operator: stable",
			},
			rdData: map[string]string{
				"precache.image":                "test-image:test-tag",
				"platform.image":                "test-platform-image:test-tag",
				"operators.indexes":             "registry.example.com:5000/test-index:v0.0",
				"operators.packagesAndChannels": "local-storage-operator: stable\nperformance-addon-operator: 4.9\nptp-operator: stable\nsriov-network-operator: stable",
			},
		},
		{
			name:     "Overrides don't exist",
			objectNs: "dummy",
			readNs:   "test",
			wrData: map[string]string{
				"precache.image":                "test-image:test_tag",
				"platform.image":                "test-platform-image:test_tag",
				"operators.indexes":             "registry.example.com:5000/test-index:v0.0",
				"operators.packagesAndChannels": "local-storage-operator: stable\nperformance-addon-operator: 4.9\nptp-operator: stable\nsriov-network-operator: stable",
			},
			rdData: map[string]string{},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}

			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.OperatorConfigOverrides,
					Namespace: tc.objectNs,
				},
				Data: tc.wrData,
			}

			if err := r.Create(context.TODO(), cm); err != nil {
				t.Errorf("error creating a configmap: %v", err)
			}
			var cgu ranv1alpha1.ClusterGroupUpgrade
			cgu.Namespace = tc.readNs
			res, err := r.getOverrides(context.TODO(), &cgu)
			if err != nil {
				t.Errorf("error reading a configmap: %v", err)
			}
			assert.Equal(t, tc.rdData, res)
		})
	}
}
