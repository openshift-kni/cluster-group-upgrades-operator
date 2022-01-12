package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	actionv1beta1 "github.com/open-cluster-management/multicloud-operators-foundation/pkg/apis/action/v1beta1"
	viewv1beta1 "github.com/open-cluster-management/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// ProcessSubscriptionManagedClusterView processes the content of a view that is configured to watch a Subscription
// type object and takes the necessary actions to approve the InstallPlan associated with that Subscription.
func ProcessSubscriptionManagedClusterView(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	clusterName string, mcv *viewv1beta1.ManagedClusterView, Log logr.Logger) (int, error) {

	conditionMCVforSub := meta.FindStatusCondition(mcv.Status.Conditions, viewv1beta1.ConditionViewProcessing)
	if conditionMCVforSub == nil {
		Log.Info("ManagedClusterView was not (yet) ready, trying again later",
			"managedcluserview", mcv.ObjectMeta.Name, "namespace", mcv.ObjectMeta.Namespace)
		return InstallPlanCannotBeApproved, nil
	}

	if conditionMCVforSub.Reason != viewv1beta1.ReasonGetResource {
		Log.Info("ManagedClusterView was not able to retrieve the requested resource (yet), trying again later",
			"managedclusterview", mcv.ObjectMeta.Name, "namespace", mcv.ObjectMeta.Namespace)
		return InstallPlanCannotBeApproved, nil
	}

	// Check that the ManagedClusterView was able to retrieve the information on the subscription.
	// We do this by checking it's status and reason.
	if conditionMCVforSub.Status == "True" && conditionMCVforSub.Reason == viewv1beta1.ReasonGetResource {
		// Get the subscription content from the ManagedClusterView.
		subscription := operatorsv1alpha1.Subscription{}
		json.Unmarshal([]byte(mcv.Status.Result.Raw), &subscription)

		// If the subscription's status is "UpgradePending" approve the installPlan. For any other value of the state, continue.
		if subscription.Status.State != SubscriptionStateUpgradePending {
			Log.Info("Subscription State is not pending upgrade",
				"subscription", subscription.ObjectMeta.Name, "namespace", subscription.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}
		Log.Info("[ProcessSubscriptionManagedClusterView] Accept InstallPlan", "name",
			subscription.Status.InstallPlanRef.Name)
		installPlanResult, err := EnsureInstallPlanIsApproved(
			ctx, c, clusterGroupUpgrade, subscription, clusterName, Log)
		if err != nil {
			return InstallPlanCannotBeApproved, err
		}
		return installPlanResult, nil
	}

	return InstallPlanCannotBeApproved, nil
}

// EnsureInstallPlanIsApproved creates a view to get all the needed information on an InstallPlan and creates an
// action to approve that plan, if the plan's approval is set to Manual.
func EnsureInstallPlanIsApproved(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	subscription operatorsv1alpha1.Subscription, clusterName string, Log logr.Logger) (int, error) {
	// Create a ManagedClusterView for the InstallPlan so that we can access its latest resourceVersion.
	mcvForInstallPlanName := GetMultiCloudObjectName(
		ManagedClusterViewPrefix, clusterGroupUpgrade,
		subscription.Status.InstallPlanRef.Kind, subscription.Status.InstallPlanRef.Name)
	Log.Info("[ProcessSubscriptionManagedClusterView] Create MCV for InstallPlan", "InstallPlan",
		subscription.Status.InstallPlanRef.Name, "ns", clusterName)
	mcvForInstallPlan, err := EnsureManagedClusterView(
		ctx, c, mcvForInstallPlanName, clusterName, "InstallPlan",
		subscription.Status.InstallPlanRef.Name, subscription.Status.InstallPlanRef.Namespace,
		clusterGroupUpgrade.Name, Log)
	if err != nil {
		return InstallPlanCannotBeApproved, err
	}

	conditionMCVforInstallPlan := meta.FindStatusCondition(
		mcvForInstallPlan.Status.Conditions, viewv1beta1.ConditionViewProcessing)
	if conditionMCVforInstallPlan == nil {
		Log.Info("ManagedClusterView was not (yet) ready, try again later",
			"managedcluserview", mcvForInstallPlan.ObjectMeta.Name, "namespace", mcvForInstallPlan.ObjectMeta.Namespace)
		return InstallPlanCannotBeApproved, err
	}

	// If the MCV has successfully retrieved the info, process its content.
	if conditionMCVforInstallPlan.Status == "True" && conditionMCVforInstallPlan.Reason == viewv1beta1.ReasonGetResource {
		// Get the InstallPlan content from the ManagedClusterView.
		installPlan := operatorsv1alpha1.InstallPlan{}
		json.Unmarshal([]byte(mcvForInstallPlan.Status.Result.Raw), &installPlan)

		// If the InstallPlan's approval is not manual, return. No action is taken for Automatic install plans.
		if installPlan.Spec.Approval != operatorsv1alpha1.ApprovalManual {
			Log.Info("InstallPlan can't be approved as it's approval is already set to Automatic",
				"installPlan", installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}

		// If the InstallPlan has already been approved, return.
		if installPlan.Spec.Approval != operatorsv1alpha1.ApprovalManual {
			Log.Info("InstallPlan has already been approved",
				"installPlan", installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
			return InstallPlanCannotBeApproved, nil
		}

		Log.Info("[ProcessSubscriptionManagedClusterView] Create ManagedClusterAction for InstallPlan", "InstallPlan",
			installPlan.ObjectMeta.Name, "namespace", installPlan.ObjectMeta.Namespace)
		// Create or update the managedClusterAction to approve the install plan.
		mcaName := GetMultiCloudObjectName(
			ManagedClusterActionPrefix, clusterGroupUpgrade, "InstallPlan", installPlan.Name)
		_, err := EnsureManagedClusterActionForInstallPlan(ctx, c, mcaName, clusterName, installPlan, Log)
		if err != nil {
			return InstallPlanCannotBeApproved, err
		}

		// Check that the ManagedClusterAction completed successfully. If so, then there is no need to also check
		// the install plan.
		time.Sleep(ManagedClusterActionWaitTimeSec * time.Second)
		err = CheckManagedClusterActionDone(ctx, c, mcaName, clusterName, Log)
		if err != nil {
			Log.Info("There was an issue with the ManagedClusterAction for InstallPlan", "error", err.Error())
			return InstallPlanCannotBeApproved, err
		}
		return InstallPlanWasApproved, nil
	}

	return InstallPlanCannotBeApproved, nil
}

// EnsureManagedClusterView creates or updates a view.
func EnsureManagedClusterView(
	ctx context.Context, c client.Client, name string, namespace string, resourceType string,
	resourceName string, resourceNamespace string, cguLabel string, Log logr.Logger) (*viewv1beta1.ManagedClusterView, error) {

	mcv := &viewv1beta1.ManagedClusterView{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, mcv)

	if err != nil {
		// If the specific managedClusterView was not found, create it.
		if errors.IsNotFound(err) {
			Log.Info("[EnsureManagedClusterView] MCV doesn't exist, create it", "name", name, "namespace", namespace)
			viewMeta := metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"openshift-cluster-group-upgrades/clusterGroupUpgrade": cguLabel,
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
		Log.Info("[EnsureManagedClusterView] MCV already exists, update it", "name", name, "namespace", namespace)
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
	// Give some time for the ManagedClusterView to retrieve the info.
	time.Sleep(ManagedClusterActionWaitTimeSec * time.Second)
	return mcv, nil
}

// EnsureManagedClusterActionForInstallPlan creates or updates an action for an InstallPlan.
func EnsureManagedClusterActionForInstallPlan(
	ctx context.Context, c client.Client, name string, namespace string,
	installPlan operatorsv1alpha1.InstallPlan, Log logr.Logger) (*actionv1beta1.ManagedClusterAction, error) {

	mcaForInstallPlan := &actionv1beta1.ManagedClusterAction{}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, mcaForInstallPlan); err != nil {
		// If the specific managedClusterAction was not found, create it.
		if errors.IsNotFound(err) {
			Log.Info("[EnsureManagedClusterActionForInstallPlan] MCA doesn't exist, create it",
				"name", name, "namespace", namespace)
			actionMeta := metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			}
			actionSpec, err := NewManagedClusterActionForInstallPlanSpec(installPlan)
			if err != nil {
				return nil, err
			}
			mcaForInstallPlan = &actionv1beta1.ManagedClusterAction{
				ObjectMeta: actionMeta,
				Spec:       actionSpec,
			}

			if err := c.Create(ctx, mcaForInstallPlan); err != nil {
				return mcaForInstallPlan, nil
			}
		}
	} else {
		// If the specific managedClusterView was found, update it.
		Log.Info("[EnsureManagedClusterActionForInstallPlan] MCA already exists, update it",
			"name", name, "namespace", namespace)
		actionSpec, err := NewManagedClusterActionForInstallPlanSpec(installPlan)
		if err != nil {
			return nil, err
		}
		mcaForInstallPlan.Spec = actionSpec

		if err := c.Update(ctx, mcaForInstallPlan); err != nil {
			return nil, err
		}
	}

	return mcaForInstallPlan, nil
}

// NewManagedClusterActionForInstallPlanSpec returns the action spec for approving an InstallPlan.
func NewManagedClusterActionForInstallPlanSpec(installPlan operatorsv1alpha1.InstallPlan) (actionv1beta1.ActionSpec, error) {
	installPlanClusterServiceVersionNames, err := json.Marshal(installPlan.Spec.ClusterServiceVersionNames)
	if err != nil {
		fmt.Printf("Error trying to Marshal InstallPlan ClusterServiceVersionNames: %s", err.Error())
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

	return actionSpec, nil
}

// CheckManagedClusterActionDone returns error for all the cases in which an action didn't manage to complete
// successfully.
func CheckManagedClusterActionDone(
	ctx context.Context, c client.Client, mcaName string, mcaNamespace string, Log logr.Logger) error {
	// Check the managedClusterAction has completed successfully.
	mca := &actionv1beta1.ManagedClusterAction{}
	err := c.Get(ctx, types.NamespacedName{Name: mcaName, Namespace: mcaNamespace}, mca)
	if err != nil {
		return err
	}
	// Check that the ManagedClusterAction has completed.
	conditionMCA := meta.FindStatusCondition(mca.Status.Conditions, actionv1beta1.ConditionActionCompleted)
	if conditionMCA == nil {
		err = fmt.Errorf("ManagedClusterAction hasn't completed yet")
		return err

	}
	// Check that the ManagedClusterAction has completed successfully.
	if conditionMCA.Reason != ManagedClusterActionReasonDone {
		if conditionMCA.Reason == actionv1beta1.ReasonUpdateResourceFailed {
			err = fmt.Errorf("ManagedClusterAction failed: %s", conditionMCA.Message)
			return err
		}
		err = fmt.Errorf("ManagedClusterAction has unexpected condition reason: %s", conditionMCA.Reason)
		return err
	}

	return nil
}

// DeleteMultiClusterObjects cleans up views associated to an UOCR.
func DeleteMultiClusterObjects(
	ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, Log logr.Logger) error {
	// Only need to delete ManagedClusterView as ManagedClusterActions get deleted automatically.
	for _, batch := range clusterGroupUpgrade.Status.RemediationPlan {
		for _, clusterName := range batch {
			var mcvLabels = map[string]string{
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
			opts := []client.ListOption{
				client.InNamespace(clusterName),
				client.MatchingLabels(mcvLabels),
			}

			mcvList := &viewv1beta1.ManagedClusterViewList{}
			if err := c.List(ctx, mcvList, opts...); err != nil {
				return err
			}

			for _, mcv := range mcvList.Items {
				Log.Info("[DeleteMultiClusterObjects] Delete ManagedClusterView", "name", mcv.Name)
				if err := c.Delete(ctx, &mcv); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// GetMultiCloudObjectName computes the name of a view or action
func GetMultiCloudObjectName(managedClusterType string, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, kind string, objectName string) string {
	return strings.ToLower(managedClusterType + "-" + clusterGroupUpgrade.Name + "-" + kind + "-" + objectName)
}
