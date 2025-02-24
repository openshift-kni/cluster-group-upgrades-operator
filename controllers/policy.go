package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *ClusterGroupUpgradeReconciler) updatePlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	policiesToUpdate := make(map[int][]string)
	for clusterName, clusterProgress := range clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress {
		if clusterProgress.State != ranv1alpha1.InProgress {
			continue
		}
		clusterNames := policiesToUpdate[*clusterProgress.PolicyIndex]
		clusterNames = append(clusterNames, clusterName)
		policiesToUpdate[*clusterProgress.PolicyIndex] = clusterNames
	}

	for index, clusterNames := range policiesToUpdate {
		policyName := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[index].Name
		policyNamespace := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[index].Namespace

		placementRuleName := utils.GetResourceName(
			clusterGroupUpgrade, fmt.Sprintf("%s-placement", policyName),
		)

		if prSafeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[utils.PrefixNameWithNamespace(policyNamespace, placementRuleName)]; ok {
			// The PR should be in the same namespace as where the policy is created
			err := r.updatePlacementRuleWithClusters(ctx, clusterNames, prSafeName, policyNamespace)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("placement object name %s not found in CGU %s", placementRuleName, clusterGroupUpgrade.Name)
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updatePlacementRuleWithClusters(
	ctx context.Context, clusterNames []string, prName, prNamespace string) error {

	placementRule := &unstructured.Unstructured{}
	placementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      prName,
		Namespace: prNamespace,
	}, placementRule)

	if err != nil {
		return err
	}

	placementRuleSpecClusters := placementRule.Object["spec"].(map[string]interface{})

	var prClusterNames []string
	var updatedClusters []map[string]interface{}
	currentClusters := placementRuleSpecClusters["clusters"]

	if currentClusters != nil {
		// Check clusterName is not already present in currentClusters
		for _, clusterEntry := range currentClusters.([]interface{}) {
			clusterMap := clusterEntry.(map[string]interface{})
			updatedClusters = append(updatedClusters, clusterMap)
			prClusterNames = append(prClusterNames, clusterMap["name"].(string))
		}
	}

	for _, clusterName := range clusterNames {
		isCurrentClusterAlreadyPresent := false
		for _, prClusterName := range prClusterNames {
			if prClusterName == clusterName {
				isCurrentClusterAlreadyPresent = true
				break
			}
		}
		if !isCurrentClusterAlreadyPresent {
			updatedClusters = append(updatedClusters, map[string]interface{}{"name": clusterName})
		}
	}

	placementRuleSpecClusters["clusters"] = updatedClusters
	placementRuleSpecClusters["clusterReplicas"] = nil

	err = r.Client.Update(ctx, placementRule)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) cleanupPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var targetNamespaces []string
	for _, policy := range clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade {
		if _, ok := utils.FindStringInSlice(targetNamespaces, policy.Namespace); !ok {
			targetNamespaces = append(targetNamespaces, policy.Namespace)
		}
	}

	errorMap := make(map[string]string)
	for _, ns := range targetNamespaces {
		placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade, nil, ns)
		if err != nil {
			return err
		}

		for _, plr := range placementRules.Items {
			placementRuleSpecClusters := plr.Object["spec"].(map[string]interface{})
			placementRuleSpecClusters["clusters"] = nil
			placementRuleSpecClusters["clusterReplicas"] = 0

			err = r.Client.Update(ctx, &plr)
			if err != nil {
				errorMap[plr.GetName()] = err.Error()
			}
		}
	}

	if len(errorMap) != 0 {
		return fmt.Errorf("errors cleaning up placement rules: %s", errorMap)
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) getPolicyByName(ctx context.Context, policyName, namespace string) (*unstructured.Unstructured, error) {
	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})

	// Look for policy.
	return foundPolicy, r.Client.Get(ctx, types.NamespacedName{Name: policyName, Namespace: namespace}, foundPolicy)
}

func updateDuplicatedManagedPoliciesInfo(managedPoliciesInfo *policiesInfo, policiesNs map[string][]string) {
	managedPoliciesInfo.duplicatedPoliciesNs = make(map[string][]string)
	for crtPolicy, crtNs := range policiesNs {
		if len(crtNs) > 1 {
			managedPoliciesInfo.duplicatedPoliciesNs[crtPolicy] = crtNs
			sort.Strings(managedPoliciesInfo.duplicatedPoliciesNs[crtPolicy])
		}
	}
}

/*
	 doManagedPoliciesExist checks that all the managedPolicies specified in the CR exist.
	   returns: true/false                   if all the policies exist or not
				policiesInfo                 managed policies info including the missing policy names,
				                             the invalid policy names and the policies present on the system
				error
*/
func (r *ClusterGroupUpgradeReconciler) doManagedPoliciesExist(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	clusters []string) (bool, policiesInfo, error) {

	childPoliciesList, err := utils.GetChildPolicies(ctx, r.Client, clusters)
	if err != nil {
		return false, policiesInfo{}, err
	}

	var managedPoliciesInfo policiesInfo
	// Go through all the child policies and split the namespace from the policy name.
	// A child policy name has the name format parent_policy_namespace.parent_policy_name
	// The policy map we are creating will be of format {"policy_name": "policy_namespace"}
	policyMap := make(map[string]string)
	// Keep inventory of all the namespaces a managed policy appears in with policyNs which
	// is of format {"policy_name": []string of policy namespaces}
	policiesNs := make(map[string][]string)
	policyEnforce := make(map[string]bool)
	policyInvalidHubTmpl := make(map[string]bool)
	for _, childPolicy := range childPoliciesList {
		policyNameArr, err := utils.GetParentPolicyNameAndNamespace(childPolicy.Name)
		if err != nil {
			r.Log.Info("[doManagedPoliciesExist] Ignoring child policy " + childPolicy.Name + "with invalid name")
			continue
		}

		// Identify policies with remediationAction enforce to ignore
		if strings.EqualFold(string(childPolicy.Spec.RemediationAction), "enforce") {
			policyEnforce[policyNameArr[1]] = true
			continue
		}

		for _, policyT := range childPolicy.Spec.PolicyTemplates {
			// Identify policies have invalid hub templates.
			// If the child configuration policy contains a string pattern "{{hub",
			// it means the hub template is invalid and fails to be processed on the hub cluster.
			if strings.Contains(string(policyT.ObjectDefinition.Raw), "{{hub") {
				policyInvalidHubTmpl[policyNameArr[1]] = true
			}
		}

		policyMap[policyNameArr[1]] = policyNameArr[0]
		utils.UpdateManagedPolicyNamespaceList(policiesNs, policyNameArr)
	}

	// If a managed policy is present in more than one namespace, raise an error and advice user to
	// fix the duplicated name.
	updateDuplicatedManagedPoliciesInfo(&managedPoliciesInfo, policiesNs)
	if len(managedPoliciesInfo.duplicatedPoliciesNs) != 0 {
		return false, managedPoliciesInfo, nil
	}

	// Go through the managedPolicies in the CR, make sure they exist and save them to the upgrade's status together with
	// their namespace.
	var managedPoliciesForUpgrade []ranv1alpha1.ManagedPolicyForUpgrade
	var managedPoliciesCompliantBeforeUpgrade []string
	clusterGroupUpgrade.Status.ManagedPoliciesNs = make(map[string]string)
	clusterGroupUpgrade.Status.ManagedPoliciesContent = make(map[string]string)

	for _, managedPolicyName := range clusterGroupUpgrade.Spec.ManagedPolicies {
		if policyEnforce[managedPolicyName] {
			r.Log.Info("Ignoring policy with remediationAction enforce", "policy", managedPolicyName)
			continue
		}

		if managedPolicyNamespace, ok := policyMap[managedPolicyName]; ok {
			// Make sure the parent policy exists and nothing happened between querying the child policies above and now.
			foundPolicy, err := r.getPolicyByName(ctx, managedPolicyName, managedPolicyNamespace)

			if err != nil {
				// If the parent policy was not found, add its name to the list of missing policies.
				if errors.IsNotFound(err) {
					managedPoliciesInfo.missingPolicies = append(managedPoliciesInfo.missingPolicies, managedPolicyName)
					continue
				} else {
					// If another error happened, return it.
					return false, managedPoliciesInfo, err
				}
			}

			// If the parent policy has invalid hub template, add its name to the list of invalid policies.
			if policyInvalidHubTmpl[managedPolicyName] {
				r.Log.Error(&utils.PolicyErr{ObjName: managedPolicyName, ErrMsg: utils.PlcHasHubTmplErr}, "Policy is invalid")
				managedPoliciesInfo.invalidPolicies = append(managedPoliciesInfo.invalidPolicies, managedPolicyName)
				continue
			}

			// If the parent policy is not valid due to missing field, add its name to the list of invalid policies.
			containsStatus, policyErr := utils.InspectPolicyObjects(foundPolicy)
			if policyErr != nil {
				r.Log.Error(policyErr, "Policy is invalid")
				managedPoliciesInfo.invalidPolicies = append(managedPoliciesInfo.invalidPolicies, managedPolicyName)
				continue
			}

			if !containsStatus {
				// Check the policy has at least one of the clusters from the CR in NonCompliant state.
				clustersNonCompliantWithPolicy := r.getClustersNonCompliantWithPolicy(clusters, foundPolicy)

				if len(clustersNonCompliantWithPolicy) == 0 {
					managedPoliciesCompliantBeforeUpgrade = append(managedPoliciesCompliantBeforeUpgrade, foundPolicy.GetName())
					managedPoliciesInfo.compliantPolicies = append(managedPoliciesInfo.compliantPolicies, foundPolicy)
					continue
				}
			}
			// Update the info on the policies used in the upgrade.
			newPolicyInfo := ranv1alpha1.ManagedPolicyForUpgrade{Name: managedPolicyName, Namespace: managedPolicyNamespace}
			managedPoliciesForUpgrade = append(managedPoliciesForUpgrade, newPolicyInfo)

			// Add the policy to the list of present policies and update the status with the policy's namespace.
			managedPoliciesInfo.presentPolicies = append(managedPoliciesInfo.presentPolicies, foundPolicy)
			clusterGroupUpgrade.Status.ManagedPoliciesNs[managedPolicyName] = managedPolicyNamespace
		} else {
			managedPoliciesInfo.missingPolicies = append(managedPoliciesInfo.missingPolicies, managedPolicyName)
		}
	}

	if len(managedPoliciesForUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade = managedPoliciesForUpgrade
	}
	if len(managedPoliciesCompliantBeforeUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesCompliantBeforeUpgrade = managedPoliciesCompliantBeforeUpgrade
	}

	// If there are missing managed policies, return.
	if len(managedPoliciesInfo.missingPolicies) != 0 || len(managedPoliciesInfo.invalidPolicies) != 0 {
		return false, managedPoliciesInfo, nil
	}

	return true, managedPoliciesInfo, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicy *unstructured.Unstructured) (string, error) {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, managedPolicy.GetNamespace(), clusterGroupUpgrade, utils.MaxObjectNameLength)
	pr := r.newBatchPlacementRule(clusterGroupUpgrade, managedPolicy.GetName(), managedPolicy.GetNamespace(), safeName, name)

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: managedPolicy.GetNamespace(),
	}, foundPlacementRule)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	} else {
		pr.SetResourceVersion(foundPlacementRule.GetResourceVersion())
		err = r.Client.Update(ctx, pr)
		if err != nil {
			return "", err
		}
	}
	return safeName, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName, policyNamespace, placementRuleName, desiredName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      placementRuleName,
			"namespace": policyNamespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
				"openshift-cluster-group-upgrades/forPolicy":                    policyName,
				utils.ExcludeFromClusterBackup:                                  "true",
			},
			"annotations": map[string]interface{}{
				utils.DesiredResourceName: utils.PrefixNameWithNamespace(policyNamespace, desiredName),
			},
		},
		"spec": map[string]interface{}{
			"clusterReplicas": 0,
		},
	}

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	return u
}

/*
getNextNonCompliantPolicyForCluster goes through all the policies in the managedPolicies list, starting with the

	policy index for the requested cluster and returns the index of the first policy that has the cluster as NonCompliant.

	returns: policyIndex the index of the next policy for which the cluster is NonCompliant or -1 if no policy found
	         error/nil
*/
func (r *ClusterGroupUpgradeReconciler) getNextNonCompliantPolicyForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, bool, error) {
	isSoaking := false
	numberOfPolicies := len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	currentPolicyIndex := startIndex
	for ; currentPolicyIndex < numberOfPolicies; currentPolicyIndex++ {
		// Get the name of the managed policy matching the current index.
		currentManagedPolicyInfo := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[currentPolicyIndex]
		currentManagedPolicy, err := r.getPolicyByName(ctx, currentManagedPolicyInfo.Name, currentManagedPolicyInfo.Namespace)
		if err != nil {
			return currentPolicyIndex, isSoaking, err
		}

		// Check if current cluster is compliant or not for its current managed policy.
		clusterStatus := r.getClusterComplianceWithPolicy(clusterName, currentManagedPolicy)

		// If the cluster is compliant for the policy or if the cluster is not matched with the policy,
		// move to the next policy index.
		if clusterStatus == utils.ClusterNotMatchedWithPolicy {
			continue
		}

		if clusterStatus == utils.ClusterStatusCompliant {
			_, ok := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]
			if !ok {
				continue
			}
			shouldSoak, err := utils.ShouldSoak(currentManagedPolicy, clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt)
			if err != nil {
				r.Log.Info(err.Error())
				continue
			}
			if !shouldSoak {
				continue
			}

			if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt.IsZero() {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Now()
			}
			isSoaking = true
			r.Log.Info("Policy is compliant but should be soaked", "cluster name", clusterName, "policyName", currentManagedPolicy.GetName())
			break
		}

		if clusterStatus == utils.ClusterStatusNonCompliant {
			break
		}
	}

	return currentPolicyIndex, isSoaking, nil
}

func (r *ClusterGroupUpgradeReconciler) handlePolicyTimeoutForCluster(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, clusterState *ranv1alpha1.ClusterState) {
	clusterProgress := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]
	if clusterProgress.PolicyIndex == nil {
		r.Log.Info("[addClustsersStatusOnTimeout] Missing index for cluster", "clusterName", clusterName, "clusterProgress", clusterProgress)
		return
	}

	policyIndex := *clusterProgress.PolicyIndex
	// Avoid panics because of index out of bound in edge cases
	if policyIndex < len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade) {
		clusterState.CurrentPolicy = &ranv1alpha1.PolicyStatus{
			Name:   clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[policyIndex].Name,
			Status: utils.ClusterStatusNonCompliant}
	}
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBinding(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, placementRuleName string, managedPolicy *unstructured.Unstructured) error {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, managedPolicy.GetNamespace(), clusterGroupUpgrade, utils.MaxObjectNameLength)
	// Ensure batch placement bindings.
	pb := r.newBatchPlacementBinding(clusterGroupUpgrade, managedPolicy.GetName(), managedPolicy.GetNamespace(), placementRuleName, safeName, name)

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: managedPolicy.GetNamespace(),
	}, foundPlacementBinding)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pb)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		pb.SetResourceVersion(foundPlacementBinding.GetResourceVersion())
		err = r.Client.Update(ctx, pb)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	policyName, policyNamespace, placementRuleName, placementBindingName, desiredName string) *unstructured.Unstructured {

	var subjects []map[string]interface{}

	subject := make(map[string]interface{})
	subject["name"] = policyName
	subject["kind"] = "Policy"
	subject["apiGroup"] = "policy.open-cluster-management.io"
	subjects = append(subjects, subject)

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      placementBindingName,
			"namespace": policyNamespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
				utils.ExcludeFromClusterBackup:                                  "true",
			},
			"annotations": map[string]interface{}{
				utils.DesiredResourceName: utils.PrefixNameWithNamespace(policyNamespace, desiredName),
			},
		},
		// With subFilter option set to restricted and bindingOverrides.remediationAction
		// set to enforce, the clusters selected by this PlacementBinding will be enforced.
		"subFilter": "restricted",
		"bindingOverrides": map[string]interface{}{
			"remediationAction": "enforce",
		},
		"placementRef": map[string]interface{}{
			"name":     placementRuleName,
			"kind":     "PlacementRule",
			"apiGroup": "apps.open-cluster-management.io",
		},
		"subjects": subjects,
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})

	return u
}

func (r *ClusterGroupUpgradeReconciler) getPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName *string, policyNamespace string) (*unstructured.UnstructuredList, error) {
	var placementRuleLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
	}
	if policyName != nil {
		placementRuleLabels["openshift-cluster-group-upgrades/forPolicy"] = *policyName
	}

	listOpts := []client.ListOption{
		client.InNamespace(policyNamespace),
		client.MatchingLabels(placementRuleLabels),
	}
	placementRulesList := &unstructured.UnstructuredList{}
	placementRulesList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRuleList",
		Version: "v1",
	})
	if err := r.List(ctx, placementRulesList, listOpts...); err != nil {
		return nil, err
	}

	return placementRulesList, nil
}

func (r *ClusterGroupUpgradeReconciler) getPlacementBindings(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyNamespace string) (*unstructured.UnstructuredList, error) {
	var placementBindingLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
	}
	listOpts := []client.ListOption{
		client.InNamespace(policyNamespace),
		client.MatchingLabels(placementBindingLabels),
	}
	placementBindingsList := &unstructured.UnstructuredList{}
	placementBindingsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBindingList",
		Version: "v1",
	})
	if err := r.List(ctx, placementBindingsList, listOpts...); err != nil {
		return nil, err
	}

	return placementBindingsList, nil
}

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesPresent []*unstructured.Unstructured) error {
	// Reconcile resources
	for _, managedPolicy := range managedPoliciesPresent {
		placementRuleName, err := r.ensureBatchPlacementRule(ctx, clusterGroupUpgrade, managedPolicy)
		if err != nil {
			return err
		}

		err = r.ensureBatchPlacementBinding(ctx, clusterGroupUpgrade, placementRuleName, managedPolicy)
		if err != nil {
			return err
		}
	}
	err := r.updateChildResourceNamesInStatus(ctx, clusterGroupUpgrade)
	return err
}

func (r *ClusterGroupUpgradeReconciler) getPolicyClusterStatus(policy *unstructured.Unstructured) []interface{} {
	policyName := policy.GetName()

	// Get the compliant status part of the policy.
	if policy.Object["status"] == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has its status missing", "policyName", policyName)
		return nil
	}

	statusObject := policy.Object["status"].(map[string]interface{})
	// If there is just one cluster in the policy's status that's missing it's compliance status, then the overall
	// policy compliance status will be missing. Log if the overall compliance status is missing, but continue.
	if statusObject["compliant"] == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has it's compliant status pending", "policyName", policyName)
	}

	// Get the policy's list of cluster compliance.
	statusCompliance := statusObject["status"]
	if statusCompliance == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has it's list of cluster statuses pending", "policyName", policyName)
		return nil
	}

	subStatus := statusCompliance.([]interface{})
	if subStatus == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy is missing it's compliance status", "policyName", policyName)
		return nil
	}

	return subStatus
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithPolicy(
	clusters []string,
	policy *unstructured.Unstructured) []string {

	var nonCompliantClusters []string

	for _, cluster := range clusters {
		compliance := r.getClusterComplianceWithPolicy(cluster, policy)
		if compliance != utils.ClusterStatusCompliant {
			nonCompliantClusters = append(nonCompliantClusters, cluster)
		}
	}
	r.Log.Info("[getClustersNonCompliantWithPolicy]", "policy: ", policy.GetName(), "clusters: ", nonCompliantClusters)
	return nonCompliantClusters
}

/*
	  getClusterComplianceWithPolicy returns the compliance of a certain cluster with a certain policy
	  based on a policy's status structure which is below. If a policy is bound to a placementRule, then
	  all the clusters bound to the policy will appear in status.status as either Compliant or NonCompliant.

	  status:
	    compliant: NonCompliant
	    placement:
	    - placementBinding: binding-policy1-common-cluster-version-policy
	      placementRule: placement-policy1-common-cluster-version-policy
	    status:
	    - clustername: spoke1
	      clusternamespace: spoke1
	      compliant: NonCompliant
	    - clustername: spoke4
	      clusternamespace: spoke4
	      compliant: NonCompliant

		returns: *string pointer to a string holding either Compliant/NonCompliant/NotMatchedWithPolicy
		         error
*/
func (r *ClusterGroupUpgradeReconciler) getClusterComplianceWithPolicy(
	clusterName string, policy *unstructured.Unstructured) string {
	// Get the status of the clusters matching the policy.
	subStatus := r.getPolicyClusterStatus(policy)
	if subStatus == nil {
		r.Log.Info(
			"[getClusterComplianceWithPolicy] Policy is missing its status, treat as NonCompliant")
		return utils.ClusterStatusNonCompliant
	}

	// Loop through all the clusters in the policy's compliance status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is Compliant, return true.
		// nolint: gocritic
		if clusterName == crtSubStatusMap["clustername"].(string) {
			if crtSubStatusMap["compliant"] == utils.ClusterStatusCompliant {
				return utils.ClusterStatusCompliant
			} else if crtSubStatusMap["compliant"] == utils.ClusterStatusNonCompliant ||
				crtSubStatusMap["compliant"] == utils.ClusterStatusPending {
				// Treat pending as non-compliant
				return utils.ClusterStatusNonCompliant
			} else if crtSubStatusMap["compliant"] == nil {
				r.Log.Info(
					"[getClusterComplianceWithPolicy] Cluster is missing its compliance status, treat as NonCompliant",
					"clusterName", clusterName, "policyName", policy.GetName())
				return utils.ClusterStatusNonCompliant
			}
		}
	}
	r.Log.Info("[getClusterComplianceWithPolicy] Cluster is not matched within this policy", "cluster", clusterName, "policyName", policy.GetName())
	return utils.ClusterNotMatchedWithPolicy
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithManagedPolicies(clusters []string, managedPolicies []*unstructured.Unstructured) map[string]bool {
	clustersNonCompliantMap := make(map[string]bool)

	// clustersNonCompliantMap will be a map of the clusters present in the CR and wether they are NonCompliant with at
	// least one managed policy.
	for _, clusterName := range clusters {
		for _, managedPolicy := range managedPolicies {
			clusterCompliance := r.getClusterComplianceWithPolicy(clusterName, managedPolicy)

			if clusterCompliance == utils.ClusterStatusNonCompliant {
				// If the cluster is NonCompliant in this current policy mark it as such and move to the next cluster.
				clustersNonCompliantMap[clusterName] = true
				break
			}
		}
	}

	return clustersNonCompliantMap
}

func (r *ClusterGroupUpgradeReconciler) arePreviousBatchesCompleteForPolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, error) {
	// Check previous batches
	for i := 0; i < len(clusterGroupUpgrade.Status.RemediationPlan)-1; i++ {
		for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[i] {
			// Start with policy index 0 as we don't keep progress info from previous batches
			nextNonCompliantPolicyIndex, isSoaking, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, batchClusterName, 0)
			if err != nil || nextNonCompliantPolicyIndex < len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade) {
				return false, isSoaking, err
			}
		}
	}
	return true, false, nil
}

/*
checkDuplicateChildResources looks up the name and desired name of the new resource in the list of resource names and the safe name map, before
adding the names to them. If duplicate (with same desired name annotation value) resource is found, it gets deleted, i.e. the new one takes precedence.

	returns: the updated childResourceNameList
*/
func (r *ClusterGroupUpgradeReconciler) checkDuplicateChildResources(ctx context.Context, safeNameMap map[string]string, childResourceNames []string, newResource *unstructured.Unstructured) ([]string, error) {
	if desiredName, ok := newResource.GetAnnotations()[utils.DesiredResourceName]; ok {
		if safeName, ok := safeNameMap[desiredName]; ok {
			if newResource.GetName() != safeName {
				// Found an object with the same object name in annotation but different from our records in the names map
				// This could happen when reconcile calls work on a stale version of CGU right after a status update from a previous reconcile
				// Or the controller pod fails to update the status after creating objects, e.g. node failure
				// Remove it as we have created a new one and updated the map
				r.Log.Info("[checkDuplicateChildResources] clean up stale child resource", "name", newResource.GetName(), "kind", newResource.GetKind())
				err := r.Client.Delete(ctx, newResource)
				if !errors.IsNotFound(err) {
					return childResourceNames, err
				}
				return childResourceNames, nil
			}
		} else {
			safeNameMap[desiredName] = newResource.GetName()
		}
	}
	childResourceNames = append(childResourceNames, newResource.GetName())
	return childResourceNames, nil
}

func (r *ClusterGroupUpgradeReconciler) updateChildResourceNamesInStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var targetNamespaces []string
	for _, policy := range clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade {
		if _, ok := utils.FindStringInSlice(targetNamespaces, policy.Namespace); !ok {
			targetNamespaces = append(targetNamespaces, policy.Namespace)
		}
	}

	placementRuleNames := make([]string, 0)
	placementBindingNames := make([]string, 0)
	for _, ns := range targetNamespaces {
		placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade, nil, ns)
		if err != nil {
			return err
		}

		for _, placementRule := range placementRules.Items {
			placementRuleNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, placementRuleNames, &placementRule)
			if err != nil {
				return err
			}
		}
		clusterGroupUpgrade.Status.PlacementRules = placementRuleNames

		placementBindings, err := r.getPlacementBindings(ctx, clusterGroupUpgrade, ns)
		if err != nil {
			return err
		}

		for _, placementBinding := range placementBindings.Items {
			placementBindingNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, placementBindingNames, &placementBinding)
			if err != nil {
				return err
			}
		}
		clusterGroupUpgrade.Status.PlacementBindings = placementBindingNames
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) rootPolicyHandlerOnUpdate(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	oldPolicy := e.ObjectOld.(*policiesv1.Policy)
	newPolicy := e.ObjectNew.(*policiesv1.Policy)

	oldClusterStatusMap := make(map[string]string)
	for _, clusterStatus := range oldPolicy.Status.Status {
		oldClusterStatusMap[clusterStatus.ClusterName] = string(clusterStatus.ComplianceState)
	}

	newClusterStatusMap := make(map[string]string)
	for _, clusterStatus := range newPolicy.Status.Status {
		newClusterStatusMap[clusterStatus.ClusterName] = string(clusterStatus.ComplianceState)
	}

	var targetClusters []string // clusters with status updated that require reconciliation

	// Add the cluster to targetClusters if its compliant status has changed or it has been deleted
	for cluster, oldStatus := range oldClusterStatusMap {
		if newStatus, ok := newClusterStatusMap[cluster]; ok && newStatus == oldStatus {
			continue
		}
		targetClusters = append(targetClusters, cluster)
	}

	// Add the cluster to targetClusters if it's newly added
	for cluster := range newClusterStatusMap {
		if _, ok := oldClusterStatusMap[cluster]; !ok {
			targetClusters = append(targetClusters, cluster)
		}
	}

	if len(targetClusters) > 0 {
		// List CGUs in all namespaces
		cgus := &ranv1alpha1.ClusterGroupUpgradeList{}
		err := r.Client.List(ctx, cgus)
		if err != nil {
			r.Log.Error(err, "[rootPolicyUpdateHandler]: fail to list ClusterGroupUpgrade")
		}

		for _, cgu := range cgus.Items {
			// The CGU is complete already, skipping
			suceededCondition := meta.FindStatusCondition(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			if suceededCondition != nil && suceededCondition.Status == metav1.ConditionTrue {
				continue
			}

			// This policy is not in this CGU, continue searching in rest of CGUs
			if _, ok := utils.FindStringInSlice(cgu.Spec.ManagedPolicies, newPolicy.Name); !ok {
				continue
			}

			// Get clusters for upgrade from this CGU
			clusters, err := r.getAllClustersForUpgrade(ctx, &cgu)
			if err != nil {
				r.Log.Error(err, "[rootPolicyUpdateHandler]: error getting the clusters bound in this ClusterGroupUpgrade")
			}

			for _, targetCluster := range targetClusters {
				if _, ok := utils.FindStringInSlice(clusters, targetCluster); ok {
					// The target cluster found in this CGU, enqueue it
					q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
						Name:      cgu.GetName(),
						Namespace: cgu.GetNamespace(),
					}})

					// To avoid enqueueing duplicate CGU
					break
				}
			}
		}
	}
}
