package controllers

import (
	"context"
	"reflect"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// getPreCachingConfig: retrieves pre-caching configuration CR
func (r *ClusterGroupUpgradeReconciler) getPreCachingConfig(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	*ranv1alpha1.PreCachingConfig, error) {

	preCachingConfig := new(ranv1alpha1.PreCachingConfig)
	preCachingConfigRef := clusterGroupUpgrade.Spec.PreCachingConfigRef

	preCachingConfigName := preCachingConfigRef.Name
	preCachingConfigNamespace := preCachingConfigRef.Namespace
	// If namespace is not specified, assume the CGU namespace
	if preCachingConfigNamespace == "" {
		preCachingConfigNamespace = clusterGroupUpgrade.Namespace
	}

	if preCachingConfigName == "" {
		r.Log.Info("getPreCachingConfigSpec: no preCachingConfig CR specified")
		return preCachingConfig, nil
	}

	err := r.Get(ctx, types.NamespacedName{Namespace: preCachingConfigNamespace,
		Name: preCachingConfigName}, preCachingConfig)

	if err != nil {
		return preCachingConfig, err
	}

	return preCachingConfig, nil
}

// getPreCachingConfigSpec: retrieves pre-caching configuration spec from the associated
// PreCachingConfig custom resource
func (r *ClusterGroupUpgradeReconciler) getPreCachingConfigSpec(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	*ranv1alpha1.PreCachingConfigSpec, error) {

	preCachingConfig, err := r.getPreCachingConfig(ctx, clusterGroupUpgrade)

	if err != nil {
		return nil, err
	}

	return &preCachingConfig.Spec, nil
}

// mapPreCachingConfigSpecToPrecachingSpec maps the given PreCachingConfigSpec object to
// a corresponding PrecachingSpec object
func (r *ClusterGroupUpgradeReconciler) mapPreCachingConfigSpecToPrecachingSpec(
	preCachingConfigSpec *ranv1alpha1.PreCachingConfigSpec) *ranv1alpha1.PrecachingSpec {

	precachingSpec := &ranv1alpha1.PrecachingSpec{}

	// Extract overrides if defined
	if !reflect.DeepEqual(preCachingConfigSpec.Overrides, ranv1alpha1.PlatformPreCachingSpec{}) {
		precachingSpec.PlatformImage = preCachingConfigSpec.Overrides.PlatformImage
		precachingSpec.OperatorsIndexes = preCachingConfigSpec.Overrides.OperatorsIndexes
		precachingSpec.OperatorsPackagesAndChannels = preCachingConfigSpec.Overrides.OperatorsPackagesAndChannels
	}

	// Extract remaining fields
	precachingSpec.ExcludePrecachePatterns = preCachingConfigSpec.ExcludePrecachePatterns
	precachingSpec.SpaceRequired = preCachingConfigSpec.SpaceRequired
	precachingSpec.AdditionalImages = preCachingConfigSpec.AdditionalImages

	return precachingSpec
}
