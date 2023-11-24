package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPreCachingConfig_getPreCachingConfigSpec(t *testing.T) {
	preCacheImage := "precache-workload-override:v:0.0"
	platformImage := "test-platform-image:test-tag"
	operatorsIndexes := []string{"registry.example.com:5000/test-index:v0.0"}
	operatorsPackagesAndChannels := []string{
		"local-storage-operator: stable",
		"performance-addon-operator: 4.9",
		"ptp-operator: stable",
		"sriov-network-operator: stable"}
	testCases := []struct {
		name                         string
		configName                   string
		configNamespace              string
		cguConfigNamespace           string
		inputPreCachingConfigSpec    ranv1alpha1.PreCachingConfigSpec
		expectedPreCachingConfigSpec ranv1alpha1.PreCachingConfigSpec
		expectedError                string
	}{
		{
			name:               "PreCachingConfigCR exists",
			configName:         "precaching-config",
			configNamespace:    "test",
			cguConfigNamespace: "test",
			inputPreCachingConfigSpec: ranv1alpha1.PreCachingConfigSpec{
				Overrides: ranv1alpha1.PlatformPreCachingSpec{
					PlatformImage:                platformImage,
					OperatorsIndexes:             operatorsIndexes,
					OperatorsPackagesAndChannels: operatorsPackagesAndChannels,
					PreCacheImage:                preCacheImage,
				},
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
			expectedPreCachingConfigSpec: ranv1alpha1.PreCachingConfigSpec{
				Overrides: ranv1alpha1.PlatformPreCachingSpec{
					PlatformImage:                platformImage,
					OperatorsIndexes:             operatorsIndexes,
					OperatorsPackagesAndChannels: operatorsPackagesAndChannels,
					PreCacheImage:                preCacheImage,
				},
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
			expectedError: "",
		},
		{
			name:               "PreCachingConfigCR does not exist",
			configName:         "precaching-config",
			configNamespace:    "foobar",
			cguConfigNamespace: "test",
			inputPreCachingConfigSpec: ranv1alpha1.PreCachingConfigSpec{
				Overrides: ranv1alpha1.PlatformPreCachingSpec{
					PlatformImage:                platformImage,
					OperatorsIndexes:             operatorsIndexes,
					OperatorsPackagesAndChannels: operatorsPackagesAndChannels,
				},
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
			expectedPreCachingConfigSpec: ranv1alpha1.PreCachingConfigSpec{},
			expectedError:                "not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}

			var cgu ranv1alpha1.ClusterGroupUpgrade
			cgu.Spec.PreCachingConfigRef.Name = tc.configName
			cgu.Spec.PreCachingConfigRef.Namespace = tc.cguConfigNamespace

			preCachingConfigCR := &ranv1alpha1.PreCachingConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1alpha1",
					Kind:       "PreCachingConfig",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.configName,
					Namespace: tc.configNamespace,
				},
				Spec: tc.inputPreCachingConfigSpec,
			}

			if err := r.Create(context.TODO(), preCachingConfigCR); err != nil {
				t.Errorf("error creating PreCachingConfig CR: %v", err)
			}

			res, err := r.getPreCachingConfigSpec(context.TODO(), &cgu)

			if tc.expectedError != "" {
				assert.ErrorContains(t, err, tc.expectedError)
			} else {
				if err != nil {
					t.Errorf("error reading PreCachingConfigCR: %v", err)
				}
				assert.Equal(t, *res, tc.expectedPreCachingConfigSpec)
			}
		})
	}
}

func TestPreCachingConfig_mapPreCachingConfigSpecToPrecachingSpec(t *testing.T) {
	testCases := []struct {
		name                   string
		preCachingConfigSpec   *ranv1alpha1.PreCachingConfigSpec
		expectedPrecachingSpec *ranv1alpha1.PrecachingSpec
	}{
		{
			name: "Complete PreCachingConfigSpec with Overrides object",
			preCachingConfigSpec: &ranv1alpha1.PreCachingConfigSpec{
				Overrides: ranv1alpha1.PlatformPreCachingSpec{
					PlatformImage:    "test-platform-image:test-tag",
					OperatorsIndexes: []string{"registry.example.com:5000/test-index:v0.0"},
					OperatorsPackagesAndChannels: []string{
						"local-storage-operator: stable",
						"performance-addon-operator: 4.9",
						"ptp-operator: stable",
						"sriov-network-operator: stable"},
				},
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
			expectedPrecachingSpec: &ranv1alpha1.PrecachingSpec{
				PlatformImage:    "test-platform-image:test-tag",
				OperatorsIndexes: []string{"registry.example.com:5000/test-index:v0.0"},
				OperatorsPackagesAndChannels: []string{
					"local-storage-operator: stable",
					"performance-addon-operator: 4.9",
					"ptp-operator: stable",
					"sriov-network-operator: stable"},
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
		},
		{
			name: "Incomplete PreCachingConfigSpec with Overrides object",
			preCachingConfigSpec: &ranv1alpha1.PreCachingConfigSpec{
				Overrides: ranv1alpha1.PlatformPreCachingSpec{
					PlatformImage:    "test-platform-image:test-tag",
					OperatorsIndexes: []string{"registry.example.com:5000/test-index:v0.0"},
					OperatorsPackagesAndChannels: []string{
						"local-storage-operator: stable",
						"performance-addon-operator: 4.9",
						"ptp-operator: stable",
						"sriov-network-operator: stable"},
				},
			},
			expectedPrecachingSpec: &ranv1alpha1.PrecachingSpec{
				PlatformImage:    "test-platform-image:test-tag",
				OperatorsIndexes: []string{"registry.example.com:5000/test-index:v0.0"},
				OperatorsPackagesAndChannels: []string{
					"local-storage-operator: stable",
					"performance-addon-operator: 4.9",
					"ptp-operator: stable",
					"sriov-network-operator: stable"},
				SpaceRequired:           "",
				AdditionalImages:        []string(nil),
				ExcludePrecachePatterns: []string(nil),
			},
		},
		{
			name: "PreCachingConfigSpec missing Overrides object",
			preCachingConfigSpec: &ranv1alpha1.PreCachingConfigSpec{
				SpaceRequired:           "15 GiB",
				AdditionalImages:        []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns: []string{"aws", "azure"},
			},
			expectedPrecachingSpec: &ranv1alpha1.PrecachingSpec{
				PlatformImage:                "",
				OperatorsIndexes:             []string(nil),
				OperatorsPackagesAndChannels: []string(nil),
				SpaceRequired:                "15 GiB",
				AdditionalImages:             []string{"image1:tag", "image2:tag"},
				ExcludePrecachePatterns:      []string{"aws", "azure"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &ClusterGroupUpgradeReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects().Build(),
				Log:    logr.Discard(),
				Scheme: scheme.Scheme,
			}

			actualPrecachingSpec := r.mapPreCachingConfigSpecToPrecachingSpec(tc.preCachingConfigSpec)

			assert.Equal(t, tc.expectedPrecachingSpec, actualPrecachingSpec)
		})
	}
}
