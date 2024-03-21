/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	yaml "sigs.k8s.io/yaml"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

const (
	ztpInstallNS             = "ztp-install"
	ztpDeployWaveAnnotation  = "ran.openshift.io/ztp-deploy-wave"
	ztpRunningLabel          = "ztp-running"
	ztpDoneLabel             = "ztp-done"
	aztpRequiredLabel        = "accelerated-ztp"
	aztpDeployServiceVariant = "full"
)

// ManagedClusterForCguReconciler reconciles a ManagedCluster object to auto create the ClusterGroupUpgrade
type ManagedClusterForCguReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=precachingconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;delete

// Reconcile the managed cluster auto create ClusterGroupUpgrade
//   - Controller watches for create event of managed cluster object. Reconciliation
//     is triggered when a new managed cluster is created
//   - When a new managed cluster is created, create ClusterGroupUpgrade CR for the
//     cluster only when it's ready and its child policies are available
//   - As created ClusterGroupUpgrade has ownReference set to its managed cluster,
//     when the managed cluster is deleted, the ClusterGroupUpgrade will be auto-deleted
//
// Note: The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ManagedClusterForCguReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	managedCluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, managedCluster); err != nil {
		if errors.IsNotFound(err) {
			// managed cluster could have been deleted
			return ctrl.Result{}, nil
		}
		// Error reading managed cluster, requeue the request
		return ctrl.Result{}, err
	}

	// Stop creating UOCR or AZTP if ztp of this cluster is done already
	if _, found := managedCluster.Labels[ztpDoneLabel]; found {
		r.Log.Info("ZTP for the cluster has completed. "+ztpDoneLabel+" label found.", "Name", managedCluster.Name)
		return doNotRequeue(), nil
	}

	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: ztpInstallNS}, clusterGroupUpgrade); err != nil {
		if !errors.IsNotFound(err) {
			// Error reading clusterGroupUpgrade, requeue the request
			return ctrl.Result{}, err
		}
	} else {
		// clusterGroupUpgrade for this cluster already exists, stop reconcile
		r.Log.Info("clusterGroupUpgrade found", "Name", clusterGroupUpgrade.Name, "Namespace", clusterGroupUpgrade.Namespace)
		return doNotRequeue(), nil
	}

	// clusterGroupUpgrade CR doesn't exist
	availableCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
		// cluster is ready
		r.Log.Info("cluster is ready", "Name", managedCluster.Name)

		// Child policies get created as soon as the placementrule/placementbinding
		// gets created and matches the parent policies to the managedcluster.
		// It takes ~45 minutes for cluster to be installed and ready.
		// At this stage, all child policies should be created.
		policies, err := utils.GetChildPolicies(ctx, r.Client, []string{managedCluster.Name})
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(policies) == 0 {
			// likely no policies were created, so no child policies found
			r.Log.Info("WARN: No child policies found for cluster", "Name", managedCluster.Name)
		}

		// create clusterGroupUpgrade
		if err := r.newClusterGroupUpgrade(ctx, managedCluster, policies); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// availableCondition is false or unknown, cluster is not ready
		return r.handleNotReadyCluster(ctx, managedCluster)
	}

	return doNotRequeue(), nil
}

func (r *ManagedClusterForCguReconciler) handleNotReadyCluster(
	ctx context.Context, managedCluster *clusterv1.ManagedCluster) (reconcile.Result, error) {
	r.Log.Info("cluster is not ready", "Name", managedCluster.Name)
	if err := r.extractAztpPolicies(ctx, managedCluster); err != nil {
		return ctrl.Result{}, err
	}
	return doNotRequeue(), nil
}

// sort map[string]int by value in ascending order, return sorted keys
func sortMapByValue(sortMap map[string]int) []string {
	var keys []string
	for key := range sortMap {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		// for equal elements, sort string alphabetically
		if sortMap[keys[i]] == sortMap[keys[j]] {
			return keys[i] < keys[j]
		}
		return sortMap[keys[i]] < sortMap[keys[j]]
	})
	return keys
}

// Create a clusterGroupUpgrade
func (r *ManagedClusterForCguReconciler) newClusterGroupUpgrade(
	ctx context.Context, cluster *clusterv1.ManagedCluster, childPolicies []policiesv1.Policy) (err error) {

	var policyWaveMap = make(map[string]int)

	// Generate a list of ordered managed policies based on the deploy wave.
	// Deploywave is a way to order deployment of policies, it's defined
	// as a annotation in policy.
	// For example,
	//   metadata:
	//     annotations:
	//       "ran.openshift.io/ztp-deploy-wave": "1"
	// The list of policies is ordered from the lowest value to the highest.
	// Policy without a wave is not managed.
	for _, cPolicy := range childPolicies {
		// Ignore policies with remediationAction enforce
		if strings.EqualFold(string(cPolicy.Spec.RemediationAction), "enforce") {
			r.Log.Info("Ignoring policy " + cPolicy.Name + " with remediationAction enforce")
			continue
		}

		deployWave, found := cPolicy.GetAnnotations()[ztpDeployWaveAnnotation]
		if found {
			deployWaveInt, err := strconv.Atoi(deployWave)
			if err != nil {
				// err convert from string to int
				return fmt.Errorf("%s in policy %s is not an interger: %s", ztpDeployWaveAnnotation, cPolicy.GetName(), err)
			}
			policyName, err := utils.GetParentPolicyNameAndNamespace(cPolicy.GetName())
			if err != nil {
				r.Log.Info("Ignoring policy " + cPolicy.Name + " with invalid name")
				continue
			}
			policyWaveMap[policyName[1]] = deployWaveInt
		}
	}

	sortedManagedPolicies := sortMapByValue(policyWaveMap)
	cguMeta := metav1.ObjectMeta{
		Name:      cluster.Name,
		Namespace: ztpInstallNS,
	}
	enable := true // default
	cguSpec := ranv1alpha1.ClusterGroupUpgradeSpec{
		Enable:          &enable,
		Clusters:        []string{cluster.Name},
		ManagedPolicies: sortedManagedPolicies,
		RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
			MaxConcurrency: 1,
		},
		Actions: ranv1alpha1.Actions{
			BeforeEnable: ranv1alpha1.BeforeEnable{
				AddClusterLabels: map[string]string{
					ztpRunningLabel: "",
				},
			},
			AfterCompletion: ranv1alpha1.AfterCompletion{
				AddClusterLabels: map[string]string{
					ztpDoneLabel: "",
				},
				DeleteClusterLabels: map[string]string{
					ztpRunningLabel: "",
				},
			},
		},
	}
	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: cguMeta,
		Spec:       cguSpec,
	}

	// set managedcluster as the owner of its created ClusterGroupUpgrade CR, so when a cluster
	// is deleted, its dependent ClusterGroupUpgrade CR will be automatically cleaned up
	if err := controllerutil.SetControllerReference(cluster, clusterGroupUpgrade, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, clusterGroupUpgrade); err != nil {
		if errors.IsNotFound(err) && strings.Contains(err.Error(), "namespace") {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ztpInstallNS,
				},
			}
			if err := r.Create(ctx, namespace); err != nil {
				r.Log.Error(err, "Fail to create namespace", "name", ztpInstallNS)
				return err
			}
			// retry
			if err := r.Create(ctx, clusterGroupUpgrade); err != nil {
				r.Log.Error(err, "Fail to create clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
				return err
			}
		}

		r.Log.Error(err, "Fail to create clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
		return err
	}

	r.Log.Info("Found ManagedCluster "+cluster.Name+" without "+ztpDoneLabel+" label. Created clusterGroupUpgrade.",
		"name", cluster.Name, "namespace", ztpInstallNS)
	return nil
}

func (r *ManagedClusterForCguReconciler) newConfigmap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string]string{},
	}
}

// Extract policies for AZTP
func (r *ManagedClusterForCguReconciler) extractAztpPolicies(ctx context.Context, managedCluster *clusterv1.ManagedCluster) (err error) {
	aztpVariant, aztpRequired := managedCluster.GetLabels()[aztpRequiredLabel]
	if !aztpRequired {
		return nil
	}
	namespace := managedCluster.GetName()
	if namespace == "local-cluster" {
		return nil
	}
	configMapName := fmt.Sprintf("%s-aztp", namespace)
	innerCmNs := os.Getenv("AZTP_INNER_CONFIGMAP_NAMESPACE")
	if innerCmNs == "" {
		innerCmNs = "ztp-profile"
	}
	innerCmName := os.Getenv("AZTP_INNER_CONFIGMAP_NAME")
	if innerCmName == "" {
		innerCmName = "ztp-post-provision"
	}
	cm := r.newConfigmap(configMapName, namespace)
	if err := r.Delete(ctx, cm, &client.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}

	policies, err := utils.GetChildPolicies(ctx, r.Client, []string{namespace})
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		r.Log.Info("no child policies found for cluster", "Name", namespace)
		return nil
	}

	objects, err := r.GetConfigurationObjects(policies)
	if err != nil {
		return err
	}
	r.Log.Info(fmt.Sprintf("found %d policies and %d objects for %s", len(policies), len(objects), namespace))
	var directlyAppliedObjects []unstructured.Unstructured
	var wrappedObjects []unstructured.Unstructured

	for _, ob := range objects {
		ob.Object["status"] = map[string]interface{}{} // remove status, we can't apply it
		groupKind := ob.GroupVersionKind().GroupKind().String()
		r.Log.Info("adding object", "groupKind", groupKind, "name", ob.GetName())
		switch groupKind {
		case "PerformanceProfile.performance.openshift.io",
			"Tuned.tuned.openshift.io",
			"Namespace",
			"CatalogSource.operators.coreos.com",
			"ContainerRuntimeConfig.machineconfiguration.openshift.io":
			directlyAppliedObjects = append(directlyAppliedObjects, ob)
		case "Subscription.operators.coreos.com":
			ob.Object["spec"].(map[string]interface{})["installPlanApproval"] = "Automatic"
			wrappedObjects = append(wrappedObjects, ob)
		default:
			wrappedObjects = append(wrappedObjects, ob)
		}
	}
	if len(wrappedObjects) > 0 {
		innerCm, err := r.wrapObjects(wrappedObjects, innerCmName, innerCmNs)
		if err != nil {
			return err
		}

		innerCmObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(innerCm)
		if err != nil {
			return err
		}
		var innerCmUns unstructured.Unstructured
		innerCmUns.SetUnstructuredContent(innerCmObj)
		directlyAppliedObjects = append(directlyAppliedObjects, innerCmUns)
	}

	var data templates.AztpTemplateData

	data.AztpImage = os.Getenv("AZTP_IMG")
	if strings.Compare(data.AztpImage, "") == 0 {
		return fmt.Errorf("aztp failure: can't retrieve image pull spec")
	}

	objects, err = templates.RenderAztpService(data, aztpVariant)
	if err != nil {
		return fmt.Errorf("failed to create aztp service manifests: %v", err)
	}
	directlyAppliedObjects = append(directlyAppliedObjects, objects...)

	cmr, err := r.wrapObjects(directlyAppliedObjects, configMapName, namespace)
	if err != nil {
		return err
	}
	// make this managedcluster owner of the configmap
	if err := controllerutil.SetControllerReference(managedCluster, cmr, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, cmr); err != nil {
		return err
	}
	return nil
}

func (r *ManagedClusterForCguReconciler) wrapObjects(objects []unstructured.Unstructured, name, namespace string) (*corev1.ConfigMap, error) {
	cm := r.newConfigmap(name, namespace)
	for _, item := range objects {
		key := strings.ToLower(fmt.Sprintf("%s-%s-%s.yaml", item.GetKind(), item.GetNamespace(), item.GetName()))
		out, err := yaml.Marshal(item.Object)
		if err != nil {
			return cm, err
		}
		cm.Data[key] = string(out)
	}
	return cm, nil
}

// GetConfigurationObjects gets encapsulated objects from configuration policies
func (r *ManagedClusterForCguReconciler) GetConfigurationObjects(policies []policiesv1.Policy) ([]unstructured.Unstructured, error) {
	var uobjects []unstructured.Unstructured

	var objects []runtime.RawExtension
	for _, pol := range policies {
		if !pol.Spec.Disabled {
			for _, template := range pol.Spec.PolicyTemplates {
				o := *template.ObjectDefinition.DeepCopy()
				objects = append(objects, o)
			}
		}
	}

	for _, ob := range objects {
		var pol policyv1.ConfigurationPolicy
		err := json.Unmarshal(ob.DeepCopy().Raw, &pol)
		if err != nil {
			r.Log.Info(err.Error())
			return uobjects, err
		}
		for _, ot := range pol.Spec.ObjectTemplates {
			var object unstructured.Unstructured
			err = object.UnmarshalJSON(ot.ObjectDefinition.DeepCopy().Raw)
			if err != nil {
				return uobjects, err
			}
			object.Object["status"] = map[string]interface{}{}
			_, specDefined := object.Object["spec"]
			if specDefined || object.GetKind() == "Namespace" {
				uobjects = append(uobjects, object)
			}
		}
	}
	return uobjects, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterForCguReconciler) SetupWithManager(mgr ctrl.Manager) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ztpInstallNS,
		},
	}
	if err := r.Create(context.TODO(), namespace); err != nil {
		// fail to create namespace
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("managedclusterForCGU").
		For(&clusterv1.ManagedCluster{},
			// watch for create event for managedcluster
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return true },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Check if the event was deleting the label "ztp-done"
					// We want to return true for that event only, and false for everything else
					_, doneLabelExistsInOld := e.ObjectOld.GetLabels()[ztpDoneLabel]
					_, doneLabelExistsInNew := e.ObjectNew.GetLabels()[ztpDoneLabel]
					_, aztpRequired := e.ObjectNew.GetLabels()[aztpRequiredLabel]

					doneLabelRemoved := doneLabelExistsInOld && !doneLabelExistsInNew

					var availableInNew, availableInOld bool
					availableCondition := meta.FindStatusCondition(e.ObjectOld.(*clusterv1.ManagedCluster).Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInOld = true
					}
					availableCondition = meta.FindStatusCondition(e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInNew = true
					}

					var hubAccepted bool
					acceptedCondition := meta.FindStatusCondition(e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
					if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
						hubAccepted = true
					}

					return (doneLabelRemoved && availableInNew) || (!availableInOld && availableInNew && !doneLabelExistsInNew) || (hubAccepted && aztpRequired)
				},
			})).
		Owns(&ranv1alpha1.ClusterGroupUpgrade{},
			// watch for delete event for owned ClusterGroupUpgrade
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return true },
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
			})).
		Complete(r)
}
