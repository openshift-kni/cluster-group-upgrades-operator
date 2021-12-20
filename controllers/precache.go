package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func (r *ClusterGroupUpgradeReconciler) reconcilePrecaching(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		// Pre-caching is required
		doneCondition := meta.FindStatusCondition(
			clusterGroupUpgrade.Status.Conditions, "PrecachingDone")
		if doneCondition != nil && doneCondition.Status == metav1.ConditionTrue {
			// Precaching is done
			return nil
		} else {
			// Precaching is required and not done
			return r.updatePrecachingStatus(ctx, clusterGroupUpgrade)
		}
	}
	// No precaching required
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updatePrecachingStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return fmt.Errorf("cannot obtain the CR cluster list: %s", err)
	}

	meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionFalse,
		Reason:  "PrecachingRequired",
		Message: "Precaching is not completed (required)"})

	clusterState := make(map[string]string)
	for _, cluster := range clusters {
		clusterCreds, err := r.getManagedClusterCredentials(ctx, cluster)
		if err != nil {
			return err
		}
		clientset, err := r.getSpokeClientset(clusterCreds)
		if err != nil {
			return err
		}
		jobStatus, err := r.getPrecacheJobState(ctx, clientset)
		if err != nil {
			return err
		}

		clusterState[cluster] = jobStatus
		if len(clusterGroupUpgrade.Status.PrecacheStatus) == 0 && jobStatus == utils.PrecachePartiallyDone {
			// This condition means that there is a pre-cache job created on the previous
			// mtce window, but there was not enough time to complete it. The UOCR was
			// deleted and re-created. In this case we delete the job and create it again
			r.deletePrecacheJob(ctx, clientset)
			if err != nil {
				return err
			}
		}
		if jobStatus == utils.PrecacheNotStarted {
			err = r.deployPrecachingWorkload(ctx, clientset, clusterGroupUpgrade, cluster)
			if err != nil {
				clusterGroupUpgrade.Status.PrecacheStatus[cluster] = utils.PrecacheFailedToStart
				return err
			}
			clusterState[cluster] = utils.PrecacheStarting
		}
	}
	clusterGroupUpgrade.Status.PrecacheStatus = make(map[string]string)
	clusterGroupUpgrade.Status.PrecacheStatus = clusterState

	if func() bool {
		for _, state := range clusterState {
			if state != utils.PrecacheSucceeded {
				return false
			}
		}
		return true
	}() {
		// Handle completion
		meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "UpgradeNotStarted",
			Message: "Precaching is completed"})
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deployPrecachingWorkload(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) error {

	err := r.createPreCacheWorkloadNamespace(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "createPreCacheWorkloadNamespace on ",
		cluster, "status", "success")
	err = r.createPreCacheWorkloadServiceAccount(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "createPreCacheWorkloadServiceAccount on ",
		cluster, "status", "success")
	err = r.createPreCacheWorkloadClusterRoleBinding(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "createPreCacheWorkloadClusterRoleBinding on ",
		cluster, "status", "success")
	spec, err := r.getPrecacheSoftwareSpec(ctx, clusterGroupUpgrade, cluster)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "getPrecacheSoftwareSpec for ",
		cluster, "status", "success")
	err = r.syncPreCacheSpecConfigMap(ctx, clientset, spec)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "syncPreCacheSpecConfigMap for ",
		cluster, "status", "success")
	image, err := r.getPrecacheimagePullSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "getPrecacheimagePullSpec for ",
		cluster, "status", "success")
	var deadline int64 = 14400 // TODO: Remove after removing from precache workload image
	err = r.createPrecacheJob(ctx, clientset, image, deadline)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "createPrecacheJob for ",
		cluster, "status", "success")
	return nil
}

// getManagedClusterCredentials gets kubeconfig of the managed cluster by name.
// returns: []byte - the cluster kubeconfig (base64 encoded bytearray)
//			error
func (r *ClusterGroupUpgradeReconciler) getManagedClusterCredentials(
	ctx context.Context,
	cluster string) ([]byte, error) {

	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(
		"%s-%s", cluster, utils.KubeconfigSecretSuffix),
		Namespace: cluster}, secret)
	if err != nil {
		return []byte{}, err
	}
	return secret.Data["kubeconfig"], nil
}

// getSpokeClientset: Connects to the spoke cluster.
// returns: *kubernetes.Clientset - API clientset
//			error
func (r *ClusterGroupUpgradeReconciler) getSpokeClientset(
	kubeconfig []byte) (*kubernetes.Clientset, error) {

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		r.Log.Error(err, "failed to create K8s config")
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		r.Log.Error(err, "failed to create K8s clientset")
	}
	return clientset, err
}

// getPrecacheJobState: Gets the pre-caching state from the spoke.
// returns: string - job state, one of "NotStarted", "Active", "Succeeded",
//                   "PartiallyDone", "UnrecoverableError", "UnforeseenStatus"
//			error
func (r *ClusterGroupUpgradeReconciler) getPrecacheJobState(
	ctx context.Context, clientset *kubernetes.Clientset) (
	string, error) {

	jobs := clientset.BatchV1().Jobs(utils.PrecacheJobNamespace)
	preCacheJob, err := jobs.Get(ctx, utils.PrecacheJobName, metav1.GetOptions{})
	if err != nil {
		if err.Error() == utils.JobNotFoundString {
			return utils.PrecacheNotStarted, nil
		}
		r.Log.Error(err, "get precache job failed")
		return "", err
	}
	if preCacheJob.Status.Active > 0 {
		return utils.PrecacheActive, nil
	}
	if preCacheJob.Status.Succeeded > 0 {
		return utils.PrecacheSucceeded, nil
	}
	for _, condition := range preCacheJob.Status.Conditions {
		if condition.Type == "Failed" && condition.Status == "True" {
			r.Log.Info("getPrecacheJobState", "condition",
				condition.String())
			if condition.Reason == "DeadlineExceeded" {
				r.Log.Info("getPrecacheJobState", "DeadlineExceeded",
					"Partially done")
				return utils.PrecachePartiallyDone, nil
			} else if condition.Reason == "BackoffLimitExceeded" {
				// return utils.PrecacheUnrecoverableError, fmt.Errorf(condition.String())
				return utils.PrecacheUnrecoverableError, nil
			}
			break
		}
	}
	jobStatus, err := json.Marshal(preCacheJob.Status)
	if err != nil {
		return "", err
	}
	return utils.PrecacheUnforeseenStatus, fmt.Errorf(string(jobStatus))
}

// makeContainerMounts: fills the precaching container mounts structure.
// returns: *[]corev1.VolumeMount - volume mount list pointer
func (r *ClusterGroupUpgradeReconciler) makeContainerMounts() *[]corev1.VolumeMount {
	var mounts []corev1.VolumeMount = []corev1.VolumeMount{
		{
			Name:      "cache",
			MountPath: "/cache",
		}, {
			Name:      "varlibcontainers",
			MountPath: "/var/lib/containers",
		}, {
			Name:      "pull",
			MountPath: "/var/lib/kubelet/config.json",
			ReadOnly:  true,
		}, {
			Name:      "config-volume",
			MountPath: "/etc/config",
			ReadOnly:  true,
		}, {
			Name:      "registries",
			MountPath: "/etc/containers/registries.conf",
			ReadOnly:  true,
		}, {
			Name:      "policy",
			MountPath: "/etc/containers/policy.json",
			ReadOnly:  true,
		}, {
			Name:      "etcdocker",
			MountPath: "/etc/docker",
			ReadOnly:  true,
		}, {
			Name:      "usr",
			MountPath: "/usr",
			ReadOnly:  true,
		},
	}
	return &mounts
}

// makePodVolumes: fills the precaching pod volumes structure.
// returns: *[]corev1.Volume - volume list pointer
func (r *ClusterGroupUpgradeReconciler) makePodVolumes() *[]corev1.Volume {
	dirType := corev1.HostPathDirectory
	fileType := corev1.HostPathFile
	var volumes []corev1.Volume = []corev1.Volume{
		{
			Name: "cache",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}, {
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "pre-cache-spec",
					},
				},
			},
		}, {
			Name: "varlibcontainers",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/containers",
					Type: &dirType,
				},
			},
		}, {
			Name: "registries",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/containers/registries.conf",
					Type: &fileType,
				},
			},
		}, {
			Name: "policy",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/containers/policy.json",
					Type: &fileType,
				},
			},
		}, {
			Name: "etcdocker",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/docker",
					Type: &dirType,
				},
			},
		}, {
			Name: "usr",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/usr",
					Type: &dirType,
				},
			},
		}, {
			Name: "pull",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/kubelet/config.json",
					Type: &fileType,
				},
			},
		},
	}
	return &volumes
}

// makeContainerEnv: fills the precaching container environment variables.
// returns: *[]corev1.EnvVar - EnvVar list pointer
func (r *ClusterGroupUpgradeReconciler) makeContainerEnv(
	deadline int64) *[]corev1.EnvVar {

	var envs []corev1.EnvVar = []corev1.EnvVar{
		{
			Name:  "pull_timeout",
			Value: strconv.FormatInt(deadline, 10),
		},
		{
			Name:  "config_volume_path",
			Value: "/etc/config",
		},
	}
	return &envs
}

// createPrecacheJob: Creates a new pre-cache job on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) createPrecacheJob(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	image string,
	deadline int64) error {

	jobs := clientset.BatchV1().Jobs(utils.PrecacheJobNamespace)
	cont := fmt.Sprintf("%s-container", utils.PrecacheJobName)
	volumes := r.makePodVolumes()
	mounts := r.makeContainerMounts()
	envs := r.makeContainerEnv(deadline)
	jobSpec := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.PrecacheJobName,
			Namespace: utils.PrecacheJobNamespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          new(int32),
			ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    cont,
							Image:   image,
							Command: []string{"/bin/bash", "-c"},
							Args:    []string{"/opt/precache/precache.sh"},
							// Args: []string{"sleep inf"},
							Env: *envs,
							SecurityContext: &corev1.SecurityContext{
								Privileged: func() *bool {
									b := true
									return &b
								}(),
								RunAsUser: new(int64),
							},
							VolumeMounts: *mounts,
						},
					},
					ServiceAccountName: utils.PrecacheServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					Volumes:            *volumes,
				},
			},
		},
	}

	_, err := jobs.Create(ctx, jobSpec, metav1.CreateOptions{})

	if err != nil {
		r.Log.Error(err, "createPrecacheJob")
		return err
	}
	r.Log.Info("createPrecacheJob", "createPrecacheJob", "success")
	return nil
}

// deletePrecacheJob: Deletes the pre-cache job on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) deletePrecacheJob(
	ctx context.Context, clientset *kubernetes.Clientset) error {

	jobs := clientset.BatchV1().Jobs(utils.PrecacheJobNamespace)
	err := jobs.Delete(ctx, utils.PrecacheJobName, metav1.DeleteOptions{})
	if err != nil {
		r.Log.Error(err, "deletePrecacheJob")
		return err
	}
	r.Log.Info("deletePrecacheJob", "deletePrecacheJob", "success")
	return nil
}

// getPrecacheimagePullSpec: Get the precaching workload image pull spec.
// returns: image - pull spec string
//          error
func (r *ClusterGroupUpgradeReconciler) getPrecacheimagePullSpec(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	string, error) {

	overrides, err := r.getOperatorConfigOverrides(ctx, clusterGroupUpgrade)
	if err != nil {
		r.Log.Error(err, "getOperatorConfigOverrides failed ")
		return "", err
	}
	image := overrides["precache.image"]
	if image == "" {
		// TODO: implement getting precaching image pull spec from CSV
		return "", fmt.Errorf("getPrecacheimagePullSpec - not implemented")
	}
	return image, nil
}

func (r *ClusterGroupUpgradeReconciler) getPrecachePlatformImageForCluster(
	ctx context.Context, clusterName string) (string, error) {

	// TODO: implement getting platform image from managed policies
	return "", fmt.Errorf("getPrecachePlatformImageForCluster - not implemented." +
		"Please use 'cluster-group-upgrade-overrides' config map" +
		"to define 'platform.image'")
}

func (r *ClusterGroupUpgradeReconciler) getPrecacheOperatorIndexesForCluster(
	ctx context.Context, clusterName string) (string, error) {

	// TODO: implement getting operator indexes from managed policies
	return "",
		fmt.Errorf("getPrecacheOperatorIndexesForCluster - not implemented." +
			"Please use 'cluster-group-upgrade-overrides' config map" +
			"to define 'operators.indexes'")
}

func (r *ClusterGroupUpgradeReconciler) getPrecacheOperatorPackagesForCluster(
	ctx context.Context, clusterName string) (string, error) {

	// TODO: implement getting operator indexes from managed policies
	return "",
		fmt.Errorf("getPrecacheOperatorPackagesForCluster - not implemented." +
			"Please use 'cluster-group-upgrade-overrides' config map" +
			"to define 'operators.packagesAndChannels'")
}

// getPrecacheSoftwareSpec: Get precaching payload spec for a cluster. It consists of
//    	several parts that together compose the precaching workload API:
//			1. platform.image (e.g. "quay.io/openshift-release-dev/ocp-release:<tag>").
//          2. operators.indexes - a list of pull specs for OLM index images
//          3. operators.packagesAndChannels - Operator packages and channels
// returns: map[string]string (softwareSpec)
//          error
func (r *ClusterGroupUpgradeReconciler) getPrecacheSoftwareSpec(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) (
	map[string]string, error) {

	rv := make(map[string]string)
	overrides, err := r.getOperatorConfigOverrides(ctx, clusterGroupUpgrade)
	if err != nil {
		r.Log.Error(err, "getOperatorConfigOverrides failed")
		return rv, err
	}
	platformImage := overrides["platform.image"]
	operatorsIndexes := overrides["operators.indexes"]
	operatorsPackagesAndChannels := overrides["operators.packagesAndChannels"]
	if platformImage == "" {
		platformImage, err = r.getPrecachePlatformImageForCluster(ctx, clusterName)
		if err != nil {
			r.Log.Error(err, "getPrecachePlatformImageForCluster failed")
			return rv, err
		}
	}
	rv["platform.image"] = platformImage

	if operatorsIndexes == "" {
		operatorsIndexes, err = r.getPrecacheOperatorIndexesForCluster(ctx, clusterName)
		if err != nil {
			r.Log.Error(err, "getPrecacheOperatorIndexesForCluster failed")
			return rv, err
		}
	}
	rv["operators.indexes"] = operatorsIndexes

	if operatorsPackagesAndChannels == "" {
		operatorsPackagesAndChannels, err = r.getPrecacheOperatorPackagesForCluster(ctx, clusterName)
		if err != nil {
			r.Log.Error(err, "getPrecacheOperatorPackagesForCluster failed")
			return rv, err
		}
	}
	rv["operators.packagesAndChannels"] = operatorsPackagesAndChannels

	return rv, err
}

// syncPreCacheSpecConfigMap: Creates or updates precache spec configmap.
// returns: error
// Note: if configmap is updated when a precache job is already running,
// the update wouldn't have any effect. Check the job status before calling.
func (r *ClusterGroupUpgradeReconciler) syncPreCacheSpecConfigMap(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	softwareSpec map[string]string) error {

	cms := clientset.CoreV1().ConfigMaps(utils.PrecacheJobNamespace)

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.PrecacheSpecCmName,
			Namespace: utils.PrecacheJobNamespace,
		},
		Data: softwareSpec,
	}

	_, err := cms.Get(ctx, utils.PrecacheSpecCmName, metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			_, err = cms.Create(ctx, cm, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("couldn't create ConfigMap: %v", err)
			}
			r.Log.Info("Created ConfigMap for", cm.Namespace, cm.Name)
		} else {
			return fmt.Errorf("failed to get ConfigMap: %v", err)
		}
	} else {
		r.Log.Info("ConfigMap exists, updating", cm.Namespace, cm.Name)
		_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("couldn't update ConfigMap: %v", err)
		}
	}
	return nil
}

// deletePreCacheSpecConfigMap: Deletes the precache spec configmap.
// returns: error
func (r *ClusterGroupUpgradeReconciler) deletePreCacheSpecConfigMap(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	cms := clientset.CoreV1().ConfigMaps(utils.PrecacheJobNamespace)
	return cms.Delete(ctx, utils.PrecacheSpecCmName, metav1.DeleteOptions{})
}

// createPreCacheWorkloadNamespace: Creates the precache workload namespace
//		on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) createPreCacheWorkloadNamespace(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {
	var ann = make(map[string]string)
	ann["workload.openshift.io/allowed"] = "management"
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.PrecacheJobNamespace,
			Annotations: ann,
		},
	}
	nss := clientset.CoreV1().Namespaces()
	_, err := nss.Update(ctx, ns, metav1.UpdateOptions{})
	return err
}

// deletePreCacheWorkloadNamespace: Deletes the precache workload namespace
//		on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) deletePreCacheWorkloadNamespace(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	nss := clientset.CoreV1().Namespaces()
	return nss.Delete(ctx, utils.PrecacheJobNamespace, metav1.DeleteOptions{})
}

// createPreCacheWorkloadServiceAccount: Creates the precache workload
//		service account on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) createPreCacheWorkloadServiceAccount(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	sa := &corev1.ServiceAccount{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{Name: utils.PrecacheServiceAccountName},
	}
	sas := clientset.CoreV1().ServiceAccounts(utils.PrecacheJobNamespace)
	_, err := sas.Update(ctx, sa, metav1.UpdateOptions{})
	return err
}

// deletePreCacheWorkloadServiceAccount: Deletes the precache workload
//		service account on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) deletePreCacheWorkloadServiceAccount(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	sas := clientset.CoreV1().ServiceAccounts(utils.PrecacheJobNamespace)
	return sas.Delete(ctx, utils.PrecacheServiceAccountName, metav1.DeleteOptions{})
}

// createPreCacheWorkloadClusterRoleBinding: Creates the precache workload
//		cluster role binding on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) createPreCacheWorkloadClusterRoleBinding(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	crbs := clientset.RbacV1().ClusterRoleBindings()
	crb := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1",
			Kind: "ClusterRoleBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf(
			"%s-crb", utils.PrecacheJobNamespace)},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      utils.PrecacheServiceAccountName,
			Namespace: utils.PrecacheJobNamespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	_, err := crbs.Update(ctx, crb, metav1.UpdateOptions{})
	return err
}

// deletePreCacheWorkloadClusterRoleBinding: Deletes the precache workload
//		cluster role binding on the spoke.
// returns: error
func (r *ClusterGroupUpgradeReconciler) deletePreCacheWorkloadClusterRoleBinding(
	ctx context.Context,
	clientset *kubernetes.Clientset) error {

	crbs := clientset.RbacV1().ClusterRoleBindings()
	return crbs.Delete(ctx, fmt.Sprintf(
		"%s-crb", utils.PrecacheJobNamespace), metav1.DeleteOptions{})
}

// err = r.deletePrecacheJob(ctx, clientset)
// if err != nil {
// 	r.Log.Error(err, "deletePrecacheJob")
// }
