package controllers

import (
	"context"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// getOverrides: reads user overrides to operator configuration
//		An example for such an override would be the pre-cache
//      workload image. It's usually taken from the operator CSV,
//      but a user might need to override it in some cases
func (r *ClusterGroupUpgradeReconciler) getOverrides(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	map[string]string, error) {

	configData := make(map[string]string)
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.OperatorConfigOverrides,
			Namespace: clusterGroupUpgrade.Namespace,
		},
		Data: configData,
	}
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return configData, nil
		}
		return configData, err
	}
	configData = found.Data
	return configData, nil
}
