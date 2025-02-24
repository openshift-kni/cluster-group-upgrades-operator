package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ConfigurationObject defines the details of an object configured through a Policy
type ConfigurationObject struct {
	Kind       string  `json:"kind,omitempty"`
	Name       string  `json:"name,omitempty"`
	APIVersion string  `json:"apiVersion,omitempty"`
	Namespace  *string `json:"namespace,omitempty"`
}

func (r *ClusterGroupUpgradeReconciler) processManagedPolicyForMonitoredObjects(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesForUpgrade []*unstructured.Unstructured) error {
	for _, managedPolicy := range managedPoliciesForUpgrade {
		// Get the policy content and create any needed ManagedClusterViews for subscription type policies.
		monitoredObjects, err := r.getMonitoredObjects(managedPolicy)
		if err != nil {
			return err
		}

		// Attempting to marshal nil objects will result in a null value showing up in the managedPoliciesContent field
		if monitoredObjects == nil {
			continue
		}

		p, err := json.Marshal(monitoredObjects)
		if err != nil {
			return err
		}
		clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicy.GetName()] = string(p)
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

		// One and only one of [object-templates, object-templates-raw] should be defined
		objectTemplatePresent := specContent[utils.ObjectTemplates] != nil
		objectTemplateRawPresent := specContent[utils.ObjectTemplatesRaw] != nil

		var marshalledObjectTemplates interface{}

		switch {
		case objectTemplatePresent && objectTemplateRawPresent:
			return nil, fmt.Errorf("[getMonitoredObjects] found both %s and %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		case !objectTemplatePresent && !objectTemplateRawPresent:
			return nil, fmt.Errorf("[getMonitoredObjects] can't find %s or %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		case objectTemplatePresent:
			marshalledObjectTemplates = specContent[utils.ObjectTemplates]
		case objectTemplateRawPresent:
			stringTemplate := utils.StripObjectTemplatesRaw(specContent[utils.ObjectTemplatesRaw].(string))

			var err error
			marshalledObjectTemplates, err = utils.StringToYaml(stringTemplate)
			if err != nil {
				return nil, fmt.Errorf("%s", utils.ConfigPlcFailRawMarshal)
			}
		default:
			return nil, fmt.Errorf("[getMonitoredObjects] can't find %s or %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		}

		objectTemplatesContent := marshalledObjectTemplates.([]interface{})
		for _, objectTemplate := range objectTemplatesContent {
			objectTemplateContent := objectTemplate.(map[string]interface{})
			if objectTemplateContent["complianceType"] == "mustnothave" {
				r.Log.Info(
					"[getMonitoredObjects] skipping object because compliance type is mustnothave",
					"policyName", managedPolicyName)
				continue
			}
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

			kind, ok := innerObjectDefinitionContent["kind"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.kind",
					"policyName", managedPolicyName)
				continue
			}

			if !isMonitoredObjectType(kind) {
				continue
			}

			_, ok = objectDefinitionMetadataContent["name"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata.name",
					"policyName", managedPolicyName)
				continue
			}

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

func (r *ClusterGroupUpgradeReconciler) processMonitoredObjects(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	for clusterName, clusterProgress := range clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress {
		if clusterProgress.State != ranv1alpha1.InProgress {
			continue
		}
		managedPolicyName := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[*clusterProgress.PolicyIndex].Name
		_, ok := clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]
		if !ok {
			// Current policy for this cluster doesn't contain any monitored object for processing, continue on to the next cluster
			continue
		}

		// If there is content saved for the current managed policy, retrieve it.
		monitoredObjects := []ConfigurationObject{}
		json.Unmarshal([]byte(clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]), &monitoredObjects)

		for _, object := range monitoredObjects {
			err := r.processMonitoredObject(ctx, clusterGroupUpgrade, object, clusterName)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) processMonitoredObject(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, object ConfigurationObject, clusterName string) error {

	// Get the managedClusterView for the monitored object contained in the current managedPolicy.
	// If missing, then return error.
	mcvName := utils.GetMultiCloudObjectName(clusterGroupUpgrade, object.Kind, object.Name)
	safeName := utils.GetSafeResourceName(mcvName, "", clusterGroupUpgrade, utils.MaxObjectNameLength)
	mcv, err := utils.EnsureManagedClusterView(
		ctx, r.Client, safeName, mcvName, clusterName, object.Kind+"."+strings.Split(object.APIVersion, "/")[0],
		object.Name, *object.Namespace, clusterGroupUpgrade.Name, clusterGroupUpgrade.Namespace)
	if err != nil {
		return err
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
			return nil
		}
		switch installPlanStatus {
		case utils.InstallPlanCannotBeApproved:
			r.Log.Info("InstallPlan for subscription could not be approved", "subscription name", object.Name)
			return nil
		case utils.MultiCloudPendingStatus:
			r.Log.Info("InstallPlan for subscription could not be approved due to a MultiCloud object pending status, "+
				"retry again later", "subscription name", object.Name)
			return nil
		case utils.InstallPlanWasApproved:
			r.Log.Info("InstallPlan for subscription was approved", "subscription name", object.Name)
		}

	case utils.ClusterVersionGroupVersionKind().Kind:
		// TODO gather useful info from CV status and update the cluster/policy status in CGU
	}
	return nil
}
