package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"strings"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
)

type precachingSpec struct {
	PlatformImage                string
	OperatorsIndexes             []string
	OperatorsPackagesAndChannels []string
}

// reconcilePrecaching: main precaching loop function
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) reconcilePrecaching(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		// Pre-caching is required
		doneCondition := meta.FindStatusCondition(
			clusterGroupUpgrade.Status.Conditions, "PrecachingDone")
		r.Log.Info("[reconcilePrecaching]",
			"FindStatusCondition  PrecachingDone", doneCondition)
		if doneCondition != nil && doneCondition.Status == metav1.ConditionTrue {
			// Precaching is done
			return nil
		}
		// Precaching is required and not marked as done
		return r.updatePrecachingStatus(ctx, clusterGroupUpgrade)
	}
	// No precaching required
	return nil
}

// updatePrecachingStatus: iterates over clusters and checks precaching status
// returns: error
func (r *ClusterGroupUpgradeReconciler) updatePrecachingStatus(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return fmt.Errorf("cannot obtain the CGU cluster list: %s", err)
	}

	meta.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "PrecachingRequired",
			Message: "Precaching is not completed (required)"})

	meta.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "PrecachingDone",
			Status:  metav1.ConditionFalse,
			Reason:  "PrecachingNotDone",
			Message: "Precaching is required and not done"})

	clusterState := make(map[string]string)
	for _, cluster := range clusters {
		clusterCreds, err := r.getManagedClusterCredentials(ctx, cluster)
		if err != nil {
			if errors.IsNotFound(err) {
				clusterState[cluster] = utils.PrecacheFailedToStart
				msg := fmt.Sprintf("%s namespace must have '%s-admin-kubeconfig' "+
					"secret. It is automatically created by the advanced cluster management "+
					"during the cluster installation", cluster, cluster)
				r.Log.Info("[updatePrecachingStatus]", "status", utils.PrecacheFailedToStart,
					"cluster", cluster, "reason", "KubeconfigNotFound", "message", msg)
				meta.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "PrecachingCanStart",
						Status:  metav1.ConditionFalse,
						Reason:  "ClusterCredentialsMissing",
						Message: msg})
				continue
			}
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
		if len(clusterGroupUpgrade.Status.PrecacheStatus) == 0 &&
			jobStatus == utils.PrecachePartiallyDone {
			// This condition means that there is a pre-cache job created on the previous
			// mtce window, but there was not enough time to complete it. The CGU was
			// deleted and re-created. In this case we delete the job and create it again
			r.deletePrecacheJob(ctx, clientset)
			if err != nil {
				return err
			}
			jobStatus = utils.PrecacheNotStarted
		}
		if jobStatus == utils.PrecacheNotStarted {
			err = r.deployPrecachingWorkload(ctx, clientset, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
			clusterState[cluster] = utils.PrecacheStarting
		}
		if clusterGroupUpgrade.Status.PrecacheStatus[cluster] != utils.PrecacheSucceeded &&
			jobStatus == utils.PrecacheSucceeded {
			// Transition from "not done" to "done"
			err = r.undeployPrecachingWorkload(ctx, clientset, clusterGroupUpgrade, cluster)
			if err != nil {
				// If everything is done but something in cleanup fails, log it and continue
				r.Log.Info("[updatePrecachingStatus]", "Cleanup error",
					err, "cluster", cluster)
			}
		}
	}
	clusterGroupUpgrade.Status.PrecacheStatus = make(map[string]string)
	clusterGroupUpgrade.Status.PrecacheStatus = clusterState

	// Handle utils.PrecacheFailedToStart alleviation
	if func() bool {
		for _, state := range clusterState {
			if state == utils.PrecacheFailedToStart {
				return false
			}
		}
		return true
	}() {
		if meta.IsStatusConditionPresentAndEqual(
			clusterGroupUpgrade.Status.Conditions, "PrecachingCanStart", metav1.ConditionFalse) {
			meta.RemoveStatusCondition(&clusterGroupUpgrade.Status.Conditions, "PrecachingCanStart")
		}
	}

	// Handle completion
	if func() bool {
		for _, state := range clusterState {
			if state != utils.PrecacheSucceeded {
				return false
			}
		}
		return true
	}() {
		meta.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "UpgradeNotStarted",
				Message: "Precaching is completed"})
		meta.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    "PrecachingDone",
				Status:  metav1.ConditionTrue,
				Reason:  "PrecachingCompleted",
				Message: "Precaching is completed"})
		meta.RemoveStatusCondition(&clusterGroupUpgrade.Status.Conditions, "PrecacheSpecValid")
	}
	return nil
}

// getPrecachingSpecFromPolicies: extract the precaching spec from policies
//		in the CGU namespace. There are three object types to look at:
//      - ClusterVersion: release image must be specified to be pre-cached
//      - Subscription: provides the list of operator packages and channels
//      - CatalogSource: must be explicitly configured to be precached.
//        All the clusters in the CGU must have same catalog source(s)
// returns: precachingSpec, error

func (r *ClusterGroupUpgradeReconciler) getPrecachingSpecFromPolicies(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (precachingSpec, error) {

	var spec precachingSpec
	policiesList, err := r.getPoliciesForNamespace(ctx, clusterGroupUpgrade.GetNamespace())
	if err != nil {
		return spec, err
	}

	for _, policy := range policiesList.Items {
		// Filter by explicit list in CGU

		if !func(a string, list []string) bool {
			for _, b := range list {
				if b == a {
					return true
				}
			}
			return false
		}(policy.GetName(), clusterGroupUpgrade.Spec.ManagedPolicies) {
			r.Log.Info("[getPrecachingSpecFromPolicies]", "Skip policy",
				policy.GetName(), "reason", "Not in CGU")
			continue
		}

		objects, err := r.stripPolicy(policy.Object)
		if err != nil {
			return spec, err
		}
		for _, object := range objects {
			kind := object["kind"]
			switch kind {
			case "ClusterVersion":
				image := object["spec"].(map[string]interface {
				})["desiredUpdate"].(map[string]interface{})["image"]
				if len(spec.PlatformImage) > 0 {
					msg := fmt.Sprintf("Platform image must be set once, but %s and %s were given",
						spec.PlatformImage, image)
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "PrecacheSpecValid",
						Status:  metav1.ConditionFalse,
						Reason:  "PlatformImageConflict",
						Message: msg})
					return *new(precachingSpec), nil
				}
				spec.PlatformImage = fmt.Sprintf("%s", image)
				r.Log.Info("[getPrecachingSpecFromPolicies]", "ClusterVersion image", image)
			case "Subscription":
				packChan := fmt.Sprintf("%s:%s", object["spec"].(map[string]interface{})["name"],
					object["spec"].(map[string]interface{})["channel"])
				spec.OperatorsPackagesAndChannels = append(spec.OperatorsPackagesAndChannels, packChan)
				r.Log.Info("[getPrecachingSpecFromPolicies]", "Operator package:channel", packChan)
				continue
			case "CatalogSource":
				index := fmt.Sprintf("%s", object["spec"].(map[string]interface{})["image"])
				spec.OperatorsIndexes = append(spec.OperatorsIndexes, index)
				r.Log.Info("[getPrecachingSpecFromPolicies]", "CatalogSource", index)
				continue
			default:
				continue
			}
		}
	}
	return spec, nil
}

// stripPolicy strips policy information and returns the underlying objects
// returns: []interface{} - list of the underlying objects in the policy
//			error
func (r *ClusterGroupUpgradeReconciler) stripPolicy(
	policyObject map[string]interface{}) ([]map[string]interface{}, error) {

	var objects []map[string]interface{}

	policyTemplates, exists, err := unstructured.NestedFieldCopy(
		policyObject, "spec", "policy-templates")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("[stripPolicy] spec -> policy-templates not found")
	}

	for _, policyTemplate := range policyTemplates.([]interface{}) {
		objTemplates := policyTemplate.(map[string]interface {
		})["objectDefinition"].(map[string]interface {
		})["spec"].(map[string]interface{})["object-templates"]
		if policyTemplate == nil {
			return nil, fmt.Errorf("[stripPolicy] can't find object-templates in policyTemplate")
		}
		for _, objTemplate := range objTemplates.([]interface{}) {
			spec := objTemplate.(map[string]interface{})["objectDefinition"]
			if spec == nil {
				return nil, fmt.Errorf("[stripPolicy] can't find any objectDefinition")
			}
			objects = append(objects, spec.(map[string]interface{}))
		}
	}
	return objects, nil
}

// deployPrecachingWorkload deploys precaching workload on the spoke
// returns: error
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
	r.Log.Info("[deployPrecachingWorkload]", "getPrecacheSoftwareSpec",
		cluster, "status", "success")

	ok, msg := r.checkPreCacheSpecConsistency(spec)
	if !ok {
		meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "PrecacheSpecValid",
			Status:  metav1.ConditionFalse,
			Reason:  "PrecacheSpecIsIncomplete",
			Message: msg})
		return nil
	}
	meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
		Type:    "PrecacheSpecValid",
		Status:  metav1.ConditionTrue,
		Reason:  "PrecacheSpecIsWellFormed",
		Message: msg})

	err = r.syncPreCacheSpecConfigMap(ctx, clientset, spec)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "syncPreCacheSpecConfigMap",
		cluster, "status", "success")
	image, err := r.getPrecacheimagePullSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "getPrecacheimagePullSpec",
		cluster, "status", "success")

	deadlineInt := clusterGroupUpgrade.Spec.RemediationStrategy.Timeout
	var deadline int64 = int64(deadlineInt * 60)
	err = r.createPrecacheJob(ctx, clientset, image, deadline)
	if err != nil {
		return err
	}
	r.Log.Info("[deployPrecachingWorkload]", "createPrecacheJob for ",
		cluster, "status", "success")
	return nil
}

// undeployPrecachingWorkload cleans up precaching manifests from the spoke leaving only the job
// returns: error
func (r *ClusterGroupUpgradeReconciler) undeployPrecachingWorkload(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) error {

	err := r.deletePreCacheWorkloadServiceAccount(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[undeployPrecachingWorkload]", "deletePreCacheWorkloadServiceAccount on ",
		cluster, "status", "success")
	err = r.deletePreCacheWorkloadClusterRoleBinding(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[undeployPrecachingWorkload]", "deletePreCacheWorkloadClusterRoleBinding on ",
		cluster, "status", "success")
	err = r.deletePreCacheSpecConfigMap(ctx, clientset)
	if err != nil {
		return err
	}
	r.Log.Info("[undeployPrecachingWorkload]", "deletePreCacheSpecConfigMap",
		cluster, "status", "success")
	return nil
}

// getMyCsv gets CGU clusterserviceversion.
// returns: []byte - the cluster kubeconfig (base64 encoded bytearray)
//			error
func (r *ClusterGroupUpgradeReconciler) getMyCsv(
	ctx context.Context) (map[string]interface{}, error) {

	config := ctrl.GetConfigOrDie()
	dynamic := dynamic.NewForConfigOrDie(config)
	resourceID := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}
	list, err := dynamic.Resource(resourceID).List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	for _, item := range list.Items {
		name := fmt.Sprintf("%s", item.Object["metadata"].(map[string]interface{})["name"])
		if strings.Contains(name, utils.CsvNamePrefix) {
			return item.Object, nil
		}
		r.Log.Info("[getMyCsv]", "item", name)
	}

	return nil, fmt.Errorf("CSV %s not found", utils.CsvNamePrefix)
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
		if errors.IsNotFound(err) {
			r.Log.Info("[getPrecacheJobState]", "return", utils.PrecacheNotStarted)
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
			Name:      "host",
			MountPath: "/host",
		}, {
			Name:      "config-volume",
			MountPath: "/etc/config",
			ReadOnly:  true,
		},
	}
	return &mounts
}

// makePodVolumes: fills the precaching pod volumes structure.
// returns: *[]corev1.Volume - volume list pointer
func (r *ClusterGroupUpgradeReconciler) makePodVolumes() *[]corev1.Volume {
	dirType := corev1.HostPathDirectory
	var volumes []corev1.Volume = []corev1.Volume{
		{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "pre-cache-spec",
					},
				},
			},
		}, {
			Name: "host",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/",
					Type: &dirType,
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
					PriorityClassName:  "system-cluster-critical",
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
		csv, err := r.getMyCsv(ctx)
		if err != nil {
			return "", err
		}
		spec := csv["spec"]
		imagesList := spec.(map[string]interface{})["relatedImages"]
		for _, item := range imagesList.([]interface{}) {
			if item.(map[string]interface{})["name"] == "pre-caching-workload" {
				return item.(map[string]interface{})["image"].(string), nil
			}
		}
		return "", fmt.Errorf("can't find pre-caching image pull spec in CGU CSV or overrides")
	}
	return image, nil
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

	spec, err := r.getPrecachingSpecFromPolicies(ctx, clusterGroupUpgrade)
	if err != nil {
		return nil, err
	}
	r.Log.Info("[getPrecacheSoftwareSpec]", "PrecacheSoftwareSpec:", spec)

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
		platformImage = spec.PlatformImage
	}
	rv["platform.image"] = platformImage

	if operatorsIndexes == "" {
		for _, entry := range spec.OperatorsIndexes {
			operatorsIndexes = fmt.Sprintln(operatorsIndexes, entry)
		}
	}
	rv["operators.indexes"] = operatorsIndexes

	if operatorsPackagesAndChannels == "" {
		for _, entry := range spec.OperatorsPackagesAndChannels {
			operatorsPackagesAndChannels = fmt.Sprintln(operatorsPackagesAndChannels, entry)
		}
	}
	rv["operators.packagesAndChannels"] = operatorsPackagesAndChannels

	return rv, err
}

// checkPreCacheSpecConsistency
// returns: consistent (bool), message (string)
func (r *ClusterGroupUpgradeReconciler) checkPreCacheSpecConsistency(
	spec map[string]string) (consistent bool, message string) {

	var operatorsRequested, platformRequested bool = true, true
	if spec["operators.indexes"] == "" {
		operatorsRequested = false
	}
	if spec["platform.image"] == "" {
		platformRequested = false
	}
	if operatorsRequested && spec["operators.packagesAndChannels"] == "" {
		return false, "inconsistent precaching configuration: olm index provided, but no packages"
	}
	if !operatorsRequested && !platformRequested {
		return false, "inconsistent precaching configuration: no software spec provided"
	}
	return true, ""
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
	if err != nil && errors.IsNotFound(err) {
		_, err = nss.Create(ctx, ns, metav1.CreateOptions{})
	}
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
	if errors.IsNotFound(err) {
		_, err = sas.Create(ctx, sa, metav1.CreateOptions{})
	}
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

// precachingCleanup: delete all precaching jobs
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) precachingCleanup(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return fmt.Errorf("[precachingCleanup]cannot obtain the CGU cluster list: %s", err)
		}
		for _, cluster := range clusters {
			clusterCreds, err := r.getManagedClusterCredentials(ctx, cluster)
			if err != nil {
				return fmt.Errorf("[precachingCleanup]cannot get cluster credentials: %s, %s", cluster, err)
			}
			clientset, err := r.getSpokeClientset(clusterCreds)
			if err != nil {
				return err
			}
			err = r.deletePrecacheJob(ctx, clientset)
			if err != nil {
				return err
			}
			err = r.deletePreCacheWorkloadNamespace(ctx, clientset)
			if err != nil {
				return err
			}
		}
	}
	// No precaching required
	return nil
}
