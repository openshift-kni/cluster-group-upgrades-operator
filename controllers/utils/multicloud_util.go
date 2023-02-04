/*
Copyright 2022.

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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	actionv1beta1 "github.com/stolostron/cluster-lifecycle-api/action/v1beta1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
)

var multiCloudLog = ctrl.Log.WithName("multiCloudLog")

// ProcessSubscriptionManagedClusterView processes the content of a view that is configured to watch a Subscription
// type object and takes the necessary actions to approve the InstallPlan associated with that Subscription.
func ProcessSubscriptionManagedClusterView(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	clusterName string, mcv *viewv1beta1.ManagedClusterView) (int, error) {

	conditionMCVforSub := meta.FindStatusCondition(mcv.Status.Conditions, viewv1beta1.ConditionViewProcessing)
	if conditionMCVforSub == nil {
		multiCloudLog.Info("ManagedClusterView was not (yet) ready, trying again later",
			"managedcluserview", mcv.ObjectMeta.Name, "namespace", mcv.ObjectMeta.Namespace)
		return MultiCloudPendingStatus, nil
	}

	if conditionMCVforSub.Reason != viewv1beta1.ReasonGetResource {
		multiCloudLog.Info("ManagedClusterView was not able to retrieve the requested resource (yet), trying again later",
			"managedclusterview", mcv.ObjectMeta.Name, "namespace", mcv.ObjectMeta.Namespace)
		return MultiCloudPendingStatus, nil
	}

	// Check that the ManagedClusterView was able to retrieve the information on the subscription.
	// We do this by checking it's status and reason.
	if conditionMCVforSub.Status == "True" && conditionMCVforSub.Reason == viewv1beta1.ReasonGetResource {
		// Get the subscription content from the ManagedClusterView.
		subscription := operatorsv1alpha1.Subscription{}
		json.Unmarshal(mcv.Status.Result.Raw, &subscription)

		// If the subscription's status is "UpgradePending" approve the installPlan. For any other value of the state, continue.
		if subscription.Status.State != SubscriptionStateUpgradePending {
			multiCloudLog.Info("Subscription State is not pending upgrade",
				"subscription", subscription.ObjectMeta.Name, "namespace", subscription.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}
		if subscription.Status.Install == nil {
			multiCloudLog.Info("Subscription status doesn't include information about the InstallPlan",
				"subscription", subscription.ObjectMeta.Name, "namespace", subscription.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}
		multiCloudLog.Info("Accept InstallPlan", "name", subscription.Status.Install.Name,
			"namespace", subscription.ObjectMeta.Namespace)
		installPlanResult, err := EnsureInstallPlanIsApproved(ctx, c, clusterGroupUpgrade, subscription, clusterName)
		if err != nil {
			return installPlanResult, err
		}
		return installPlanResult, nil
	}

	return InstallPlanCannotBeApproved, nil
}

// EnsureInstallPlanIsApproved creates a view to get all the needed information on an InstallPlan and creates an
// action to approve that plan, if the plan's approval is set to Manual.
var EnsureInstallPlanIsApproved = func(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
	// Create a ManagedClusterView for the InstallPlan so that we can access its latest resourceVersion.
	mcvForInstallPlanName := GetMultiCloudObjectName(
		clusterGroupUpgrade, subscription.Status.Install.Kind, subscription.Status.Install.Name)
	if len(mcvForInstallPlanName) > MaxObjectNameLength {
		// example name: cguName-cguNamespace-installplan-install-kw2bs
		// truncate before "-installplan-install-kw2bs"
		// len("-installplan-install-kw2bs") = 26
		mcvForInstallPlanName = mcvForInstallPlanName[:MaxObjectNameLength-26] + mcvForInstallPlanName[len(mcvForInstallPlanName)-26:]
	}
	multiCloudLog.Info("[EnsureInstallPlanIsApproved] Create MCV for InstallPlan", "InstallPlan",
		subscription.Status.Install.Name, "ns", clusterName)
	mcvForInstallPlan, err := EnsureManagedClusterView(
		ctx, c, mcvForInstallPlanName, mcvForInstallPlanName, clusterName, "InstallPlan", subscription.Status.Install.Name,
		subscription.ObjectMeta.Namespace, clusterGroupUpgrade.Namespace+"-"+clusterGroupUpgrade.Name)
	if err != nil {
		return InstallPlanCannotBeApproved, err
	}

	conditionMCVforInstallPlan := meta.FindStatusCondition(
		mcvForInstallPlan.Status.Conditions, viewv1beta1.ConditionViewProcessing)
	if conditionMCVforInstallPlan == nil {
		multiCloudLog.Info("ManagedClusterView was not (yet) ready, try again later",
			"managedclusterview", mcvForInstallPlan.ObjectMeta.Name, "namespace", mcvForInstallPlan.ObjectMeta.Namespace)
		return MultiCloudPendingStatus, nil
	}

	if conditionMCVforInstallPlan.Reason != viewv1beta1.ReasonGetResource {
		multiCloudLog.Info("ManagedClusterView was not able to retrieve the requested resource (yet), trying again later",
			"managedclusterview", mcvForInstallPlan.ObjectMeta.Name, "namespace", mcvForInstallPlan.ObjectMeta.Namespace)
		return MultiCloudPendingStatus, nil
	}

	// If the MCV has successfully retrieved the info, process its content.
	if conditionMCVforInstallPlan.Status == "True" && conditionMCVforInstallPlan.Reason == viewv1beta1.ReasonGetResource {
		// Get the InstallPlan content from the ManagedClusterView.
		installPlan := operatorsv1alpha1.InstallPlan{}
		json.Unmarshal(mcvForInstallPlan.Status.Result.Raw, &installPlan)

		// If the InstallPlan's approval is not manual, return. No action is taken for Automatic install plans.
		if installPlan.Spec.Approval != operatorsv1alpha1.ApprovalManual {
			multiCloudLog.Info("InstallPlan can't be approved as it's approval is not set to Manual",
				"InstallPlan", installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}

		// If the InstallPlan has already been approved, return.
		if installPlan.Spec.Approved {
			multiCloudLog.Info("InstallPlan has already been approved",
				"InstallPlan", installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
			return InstallPlanAlreadyApproved, nil
		}

		multiCloudLog.Info("Create ManagedClusterAction for InstallPlan", "InstallPlan",
			installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
		// Create or update the managedClusterAction to approve the install plan.
		mcaName := GetMultiCloudObjectName(clusterGroupUpgrade, "InstallPlan", installPlan.Name)
		safeName := GetSafeResourceName(mcaName, clusterGroupUpgrade, MaxObjectNameLength, 0)
		_, err := EnsureManagedClusterActionForInstallPlan(ctx, c, safeName, mcaName, clusterName, installPlan)
		if err != nil {
			return InstallPlanCannotBeApproved, err
		}

		return InstallPlanWasApproved, nil
	}

	return InstallPlanCannotBeApproved, nil
}

// EnsureManagedClusterView creates or updates a view.
func EnsureManagedClusterView(
	ctx context.Context, c client.Client, safeName, name, namespace, resourceType,
	resourceName, resourceNamespace, cguLabel string) (*viewv1beta1.ManagedClusterView, error) {

	mcv := &viewv1beta1.ManagedClusterView{}
	err := c.Get(ctx, types.NamespacedName{Name: safeName, Namespace: namespace}, mcv)

	if err != nil {
		// If the specific managedClusterView was not found, create it.
		if errors.IsNotFound(err) {
			multiCloudLog.Info("[EnsureManagedClusterView] MCV doesn't exist, create it", "name", safeName, "namespace", namespace)
			viewMeta := metav1.ObjectMeta{
				Name:      safeName,
				Namespace: namespace,
				Labels: map[string]string{
					"openshift-cluster-group-upgrades/clusterGroupUpgrade": cguLabel,
				},
				Annotations: map[string]string{
					DesiredResourceName: name,
				},
			}
			viewSpec := viewv1beta1.ViewSpec{
				Scope: viewv1beta1.ViewScope{
					Resource:  resourceType,
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
			}
			mcv = &viewv1beta1.ManagedClusterView{
				ObjectMeta: viewMeta,
				Spec:       viewSpec,
			}

			if err := c.Create(ctx, mcv); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		// If the specific managedClusterView was found, update it.
		multiCloudLog.Info("[EnsureManagedClusterView] MCV already exists, update it", "name", safeName, "namespace", namespace)
		// Make sure labels contain the referral to the CGU.
		labels := mcv.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"] = cguLabel
		mcv.SetLabels(labels)

		viewSpec := viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Resource:  resourceType,
				Name:      resourceName,
				Namespace: resourceNamespace,
			},
		}

		mcv.Spec = viewSpec
		if err := c.Update(ctx, mcv); err != nil {
			return nil, err
		}
	}
	return mcv, nil
}

// EnsureManagedClusterActionForInstallPlan creates or updates an action for an InstallPlan.
func EnsureManagedClusterActionForInstallPlan(
	ctx context.Context, c client.Client, safeName, name, namespace string,
	installPlan operatorsv1alpha1.InstallPlan) (*actionv1beta1.ManagedClusterAction, error) {

	mcaForInstallPlan := &actionv1beta1.ManagedClusterAction{}
	if err := c.Get(ctx, types.NamespacedName{Name: safeName, Namespace: namespace}, mcaForInstallPlan); err != nil {
		// If the specific managedClusterAction was not found, create it.
		if errors.IsNotFound(err) {
			multiCloudLog.Info("[EnsureManagedClusterActionForInstallPlan] MCA doesn't exist, create it",
				"name", safeName, "namespace", namespace)
			actionMeta := metav1.ObjectMeta{
				Name:      safeName,
				Namespace: namespace,
				Annotations: map[string]string{
					DesiredResourceName: name,
				},
			}
			actionSpec, err := NewManagedClusterActionForInstallPlanSpec(installPlan)
			if err != nil {
				return nil, err
			}
			mcaForInstallPlan = &actionv1beta1.ManagedClusterAction{
				ObjectMeta: actionMeta,
				Spec:       *actionSpec,
			}

			if err := c.Create(ctx, mcaForInstallPlan); err != nil {
				return mcaForInstallPlan, nil
			}
		} else {
			return nil, err
		}
	} else {
		// If the specific managedClusterAction was found, don't do anything.
		multiCloudLog.Info("[EnsureManagedClusterActionForInstallPlan] MCA already exists, continue",
			"name", safeName, "namespace", namespace)
	}

	return mcaForInstallPlan, nil
}

// NewManagedClusterActionForInstallPlanSpec returns the action spec for approving an InstallPlan.
func NewManagedClusterActionForInstallPlanSpec(installPlan operatorsv1alpha1.InstallPlan) (*actionv1beta1.ActionSpec, error) {
	installPlanClusterServiceVersionNames, err := json.Marshal(installPlan.Spec.ClusterServiceVersionNames)
	if err != nil {
		return nil, fmt.Errorf("error trying to Marshal InstallPlan ClusterServiceVersionNames: %s", err.Error())
	}

	templateContent := fmt.Sprintf(
		`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "InstallPlan",
		  "metadata": {"name": "%s","resourceVersion": "%s"},
		  "spec": {"approval": "Manual","approved": true, "clusterServiceVersionNames": %s}}`,
		installPlan.ObjectMeta.Name,
		installPlan.ObjectMeta.ResourceVersion,
		string(installPlanClusterServiceVersionNames))

	actionSpec := actionv1beta1.ActionSpec{
		ActionType: actionv1beta1.UpdateActionType,
		KubeWork: &actionv1beta1.KubeWorkSpec{
			Resource:       "installplan",
			Namespace:      installPlan.ObjectMeta.Namespace,
			ObjectTemplate: runtime.RawExtension{Raw: []byte(templateContent)},
		},
	}

	return &actionSpec, nil
}

// DeleteMultiCloudObjects cleans up views associated to a cluster.
func DeleteMultiCloudObjects(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) error {
	// Only need to delete ManagedClusterView as ManagedClusterActions get deleted automatically.

	var mcvLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Namespace + "-" + clusterGroupUpgrade.Name}
	opts := []client.ListOption{
		client.InNamespace(clusterName),
		client.MatchingLabels(mcvLabels),
	}

	mcvList := &viewv1beta1.ManagedClusterViewList{}
	if err := c.List(ctx, mcvList, opts...); err != nil {
		return err
	}

	for _, mcv := range mcvList.Items {
		multiCloudLog.Info("[DeleteMultiClusterObjects] Delete ManagedClusterView", "name", mcv.Name)
		if err := c.Delete(ctx, &mcv); err != nil {
			return err
		}
	}

	return nil
}

// DeleteAllMultiCloudObjects cleans up views associated to all clusters in the list.
func DeleteAllMultiCloudObjects(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, allClustersForUpgrade []string) error {
	// Only need to delete ManagedClusterView as ManagedClusterActions get deleted automatically.
	for _, clusterName := range allClustersForUpgrade {
		err := DeleteMultiCloudObjects(ctx, c, clusterGroupUpgrade, clusterName)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetMultiCloudObjectName computes the name of a view or action
func GetMultiCloudObjectName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, kind, objectName string) string {
	return strings.ToLower(clusterGroupUpgrade.Name + "-" + clusterGroupUpgrade.Namespace + "-" + kind + "-" + objectName)
}

// GetMCVUpdateInterval computes a reasonable value based on the number of clusters
func GetMCVUpdateInterval(totalClusters int) int {
	interval := ViewUpdateSecPerCluster * totalClusters
	if interval > ViewUpdateSecTotalMax {
		interval = ViewUpdateSecTotalMax
	} else if interval < ViewUpdateSecTotalMin {
		interval = ViewUpdateSecTotalMin
	}
	return interval
}
