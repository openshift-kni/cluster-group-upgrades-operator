package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	viewv1beta1 "github.com/open-cluster-management/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// ConfigurationObject defines the details of an object configured through a Policy
type ConfigurationObject struct {
	Kind       string  `json:"kind,omitempty"`
	Name       string  `json:"name,omitempty"`
	APIVersion string  `json:"apiVersion,omitempty"`
	Namespace  *string `json:"namespace,omitempty"`
}

func (r *ClusterGroupUpgradeReconciler) processManagedPolicyForMonitoredObjects(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesForUpgrade []*unstructured.Unstructured) error {
	for _, managedPolicy := range managedPoliciesForUpgrade {
		// Get the policy content and create any needed ManagedClusterViews for subscription type policies.
		monitoredObjects, err := r.getMonitoredObjects(managedPolicy)
		if err != nil {
			return err
		}

		p, err := json.Marshal(monitoredObjects)
		if err != nil {
			return err
		}
		clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicy.GetName()] = string(p)
		r.createManagedClusterView(ctx, clusterGroupUpgrade, managedPolicy, monitoredObjects)
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) getMonitoredObjects(managedPolicy *unstructured.Unstructured) ([]ConfigurationObject, error) {
	managedPolicyName := managedPolicy.GetName()
	specObject := managedPolicy.Object["spec"].(map[string]interface{})

	// Get the policy templates.
	policyTemplates := specObject["policy-templates"]
	policyTemplatesArr := policyTemplates.([]interface{})
	var objects []ConfigurationObject

	// Go through the template array.
	for _, template := range policyTemplatesArr {
		// Get to the metadata name of the ConfigurationPolicy.
		objectDefinition := template.(map[string]interface{})["objectDefinition"]
		if objectDefinition == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition", managedPolicyName)
		}
		objectDefinitionContent := objectDefinition.(map[string]interface{})

		// Get the spec.
		spec := objectDefinitionContent["spec"]
		if spec == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec", managedPolicyName)
		}

		// Get the object-templates from the spec.
		specContent := spec.(map[string]interface{})
		objectTemplates := specContent["object-templates"]
		if objectTemplates == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec.object-templates", managedPolicyName)
		}

		objectTemplatesContent := objectTemplates.([]interface{})
		for _, objectTemplate := range objectTemplatesContent {
			objectTemplateContent := objectTemplate.(map[string]interface{})
			innerObjectDefinition := objectTemplateContent["objectDefinition"]
			if innerObjectDefinition == nil {
				return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec.object-templates.objectDefinition", managedPolicyName)
			}

			innerObjectDefinitionContent := innerObjectDefinition.(map[string]interface{})
			// Get the object's metadata.
			objectDefinitionMetadata := innerObjectDefinitionContent["metadata"]
			if objectDefinitionMetadata == nil {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata",
					"policyName", managedPolicyName)
				continue
			}

			objectDefinitionMetadataContent := innerObjectDefinitionContent["metadata"].(map[string]interface{})
			// Save the kind, name and namespace if they exist and if kind is of Subscription type.
			// If kind is missing, log and skip.
			kind, ok := innerObjectDefinitionContent["kind"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.kind",
					"policyName", managedPolicyName)
				continue
			}

			// Filter only Subscription templates.
			if !isMonitoredObjectType(kind) {
				r.Log.Info(
					"[getPolicyContent] Policy spec.policy-templates.objectDefinition.spec.object-templates.kind does not need to be monitored",
					"policyName", managedPolicyName)
				continue
			}

			// If name is missing, log and skip. We need Subscription name in order to have a valid content for
			// Subscription InstallPlan approval.
			_, ok = objectDefinitionMetadataContent["name"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata.name",
					"policyName", managedPolicyName)
				continue
			}

			// If namespace is missing, log and skip.
			_, ok = objectDefinitionMetadataContent["namespace"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata.namespace",
					"policyName", managedPolicyName)
				continue
			}

			var object ConfigurationObject
			object.Kind = innerObjectDefinitionContent["kind"].(string)
			object.Name = objectDefinitionMetadataContent["name"].(string)
			object.APIVersion = innerObjectDefinitionContent["apiVersion"].(string)
			namespace := objectDefinitionMetadataContent["namespace"].(string)
			object.Namespace = &namespace

			objects = append(objects, object)
		}

	}

	return objects, nil
}

func isMonitoredObjectType(kind interface{}) bool {
	// TODO add utils.ClusterVersionGroupVersionKind().Kind
	if kind == utils.SubscriptionGroupVersionKind().Kind {
		return true
	}
	return false
}

func (r *ClusterGroupUpgradeReconciler) createManagedClusterView(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured, objects []ConfigurationObject) error {

	nonCompliantClusters, err := r.getClustersNonCompliantWithPolicy(ctx, clusterGroupUpgrade, policy)
	if err != nil {
		return err
	}

	// Check if the current policy is also a subscription policy.
	for _, object := range objects {
		// Compute the name of the managedClusterView
		managedClusterViewName := utils.GetMultiCloudObjectName(clusterGroupUpgrade, object.Kind, object.Name)
		safeName := utils.GetSafeResourceName(managedClusterViewName, clusterGroupUpgrade, utils.MaxObjectNameLength, 0)

		// Create managedClusterView in each of the NonCompliant managed clusters' namespaces to access information for the policy.
		for _, nonCompliantCluster := range nonCompliantClusters {
			_, err = utils.EnsureManagedClusterView(
				ctx, r.Client, safeName, managedClusterViewName, nonCompliantCluster, object.Kind+"."+strings.Split(object.APIVersion, "/")[0],
				object.Name, *object.Namespace, clusterGroupUpgrade.Namespace+"-"+clusterGroupUpgrade.Name)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) processMonitoredObjects(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {

	reconcileSooner := false
	for clusterName, clusterProgress := range clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress {
		if clusterProgress.State != ranv1alpha1.InProgress {
			continue
		}
		managedPolicyName := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[*clusterProgress.PolicyIndex].Name

		// If there is no monitored object saved for the current managed policy, return.
		_, ok := clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]
		if !ok {
			r.Log.Info("[approveInstallPlan] No content for policy", "managedPolicyName", managedPolicyName)
			return false, nil
		}

		// If there is content saved for the current managed policy, retrieve it.
		monitoredObjects := []ConfigurationObject{}
		json.Unmarshal([]byte(clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]), &monitoredObjects)

		for _, object := range monitoredObjects {
			sooner, err := r.processMonitoredObject(ctx, clusterGroupUpgrade, object, clusterName)
			if err != nil {
				return reconcileSooner, err
			}
			if sooner {
				reconcileSooner = true
			}
		}
	}
	return reconcileSooner, nil
}

func (r *ClusterGroupUpgradeReconciler) processMonitoredObject(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, object ConfigurationObject, clusterName string) (bool, error) {

	// Get the managedClusterView for the monitored object contained in the current managedPolicy.
	// If missing, then return error.
	mcvName := utils.GetMultiCloudObjectName(clusterGroupUpgrade, object.Kind, object.Name)
	safeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[mcvName]
	if !ok {
		r.Log.Info("ManagedClusterView name should have been present, but it was not found")
		return false, nil
	}
	mcv := &viewv1beta1.ManagedClusterView{}
	if err := r.Get(ctx, types.NamespacedName{Name: safeName, Namespace: clusterName}, mcv); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("ManagedClusterView should have been present, but it was not found")
			return false, nil
		}
		return false, err
	}

	switch object.Kind {
	case utils.SubscriptionGroupVersionKind().Kind:
		r.Log.Info("[approveInstallPlan] Attempt to approve install plan for subscription",
			"name", object.Name, "in namespace", object.Namespace)
		// If the specific managedClusterView was found, check that it's condition Reason is "GetResourceProcessing"
		installPlanStatus, err := utils.ProcessSubscriptionManagedClusterView(
			ctx, r.Client, clusterGroupUpgrade, clusterName, mcv)
		// If there is an error in trying to approve the install plan, just print the error and continue.
		if err != nil {
			r.Log.Info("An error occurred trying to approve install plan", "error", err.Error())
			return false, nil
		}
		if installPlanStatus == utils.InstallPlanCannotBeApproved {
			r.Log.Info("InstallPlan for subscription could not be approved", "subscription name", object.Name)
			return true, nil
		} else if installPlanStatus == utils.MultiCloudPendingStatus {
			r.Log.Info("InstallPlan for subscription could not be approved due to a MultiCloud object pending status, "+
				"retry again later", "subscription name", object.Name)
			return true, nil
		} else if installPlanStatus == utils.InstallPlanWasApproved {
			r.Log.Info("InstallPlan for subscription was approved", "subscription name", object.Name)
		}

	case utils.ClusterVersionGroupVersionKind().Kind:
		// TODO gather useful info from CV status and update the cluster/policy status in CGU
	}
	return false, nil
}
