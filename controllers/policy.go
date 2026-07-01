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

func (r *ClusterGroupUpgradeReconciler) updatePlacements(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

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

		placementName := utils.GetResourceName(
			clusterGroupUpgrade, fmt.Sprintf("%s-placement", policyName),
		)

		if placementSafeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[utils.PrefixNameWithNamespace(policyNamespace, placementName)]; ok {
			// The Placement should be in the same namespace as where the policy is created
			err := r.updatePlacementWithClusters(ctx, clusterNames, placementSafeName, policyNamespace)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("placement object name %s not found in CGU %s", placementName, clusterGroupUpgrade.Name)
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updatePlacementWithClusters(
	ctx context.Context, clusterNames []string, placementName, placementNamespace string) error {

	placement := &unstructured.Unstructured{}
	placement.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.open-cluster-management.io",
		Kind:    "Placement",
		Version: "v1beta1",
	})
	err := r.Get(ctx, client.ObjectKey{
		Name:      placementName,
		Namespace: placementNamespace,
	}, placement)

	if err != nil {
		return err
	}

	// Get existing cluster names from the Placement
	existingNames, err := getPlacementClusterNames(placement)
	if err != nil {
		return err
	}

	// Build a set of existing names for deduplication
	existingSet := make(map[string]bool)
	for _, name := range existingNames {
		existingSet[name] = true
	}

	// Add new cluster names that aren't already present
	updatedNames := existingNames
	for _, clusterName := range clusterNames {
		if !existingSet[clusterName] {
			updatedNames = append(updatedNames, clusterName)
		}
	}

	// Update the Placement with the new cluster list
	err = setPlacementClusterNames(placement, updatedNames)
	if err != nil {
		return err
	}

	err = r.Update(ctx, placement)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) cleanupPlacements(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var targetNamespaces []string
	for _, policy := range clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade {
		if _, ok := utils.FindStringInSlice(targetNamespaces, policy.Namespace); !ok {
			targetNamespaces = append(targetNamespaces, policy.Namespace)
		}
	}

	errorMap := make(map[string]string)
	for _, ns := range targetNamespaces {
		placements, err := r.getPlacements(ctx, clusterGroupUpgrade, nil, ns)
		if err != nil {
			return err
		}

		for _, placement := range placements.Items {
			// Reset cluster values to empty list
			err = setPlacementClusterNames(&placement, nil)
			if err != nil {
				errorMap[placement.GetName()] = err.Error()
				continue
			}

			err = r.Update(ctx, &placement)
			if err != nil {
				errorMap[placement.GetName()] = err.Error()
			}
		}
	}

	if len(errorMap) != 0 {
		return fmt.Errorf("errors cleaning up placements: %s", errorMap)
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
	return foundPolicy, r.Get(ctx, types.NamespacedName{Name: policyName, Namespace: namespace}, foundPolicy)
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
				}
				// If another error happened, return it.
				return false, managedPoliciesInfo, err
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

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacement(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicy *unstructured.Unstructured) (string, error) {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, managedPolicy.GetNamespace(), clusterGroupUpgrade, utils.MaxObjectNameLength)
	placement := r.newBatchPlacement(clusterGroupUpgrade, managedPolicy.GetName(), managedPolicy.GetNamespace(), safeName, name)

	foundPlacement := &unstructured.Unstructured{}
	foundPlacement.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.open-cluster-management.io",
		Kind:    "Placement",
		Version: "v1beta1",
	})

	err := r.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: managedPolicy.GetNamespace(),
	}, foundPlacement)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, placement)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	} else {
		placement.SetResourceVersion(foundPlacement.GetResourceVersion())
		err = r.Update(ctx, placement)
		if err != nil {
			return "", err
		}
	}
	return safeName, nil
}

// getPlacementClusterNames extracts cluster names from Placement spec.predicates[0].requiredClusterSelector.labelSelector.matchExpressions[0].values
func getPlacementClusterNames(placement *unstructured.Unstructured) ([]string, error) {
	spec, found, err := unstructured.NestedMap(placement.Object, "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("spec not found in Placement")
	}

	predicates, found, err := unstructured.NestedSlice(spec, "predicates")
	if err != nil || !found || len(predicates) == 0 {
		return []string{}, nil
	}

	predicate, ok := predicates[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid predicate structure")
	}

	matchExpressions, found, err := unstructured.NestedSlice(predicate, "requiredClusterSelector", "labelSelector", "matchExpressions")
	if err != nil || !found || len(matchExpressions) == 0 {
		return []string{}, nil
	}

	expr, ok := matchExpressions[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid matchExpression structure")
	}

	valuesInterface, found := expr["values"]
	if !found {
		return []string{}, nil
	}

	// Convert []interface{} to []string
	valuesSlice, ok := valuesInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("values is not a slice")
	}

	values := make([]string, len(valuesSlice))
	for i, v := range valuesSlice {
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("value at index %d is not a string", i)
		}
		values[i] = str
	}

	return values, nil
}

// setPlacementClusterNames sets cluster names in Placement spec.predicates[0].requiredClusterSelector.labelSelector.matchExpressions[0].values
func setPlacementClusterNames(placement *unstructured.Unstructured, clusterNames []string) error {
	// Convert []string to []interface{} for unstructured
	var values []interface{}
	if clusterNames != nil {
		values = make([]interface{}, len(clusterNames))
		for i, name := range clusterNames {
			values[i] = name
		}
	} else {
		values = []interface{}{}
	}

	// Navigate through the nested structure and update values
	spec, found, err := unstructured.NestedFieldNoCopy(placement.Object, "spec")
	if err != nil || !found {
		return fmt.Errorf("spec not found in Placement")
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return fmt.Errorf("spec is not a map")
	}

	predicates, found := specMap["predicates"]
	if !found {
		return fmt.Errorf("predicates not found in Placement")
	}

	predicatesSlice, ok := predicates.([]interface{})
	if !ok || len(predicatesSlice) == 0 {
		return fmt.Errorf("predicates is not a valid slice")
	}

	predicate, ok := predicatesSlice[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid predicate structure")
	}

	rcs, found := predicate["requiredClusterSelector"]
	if !found {
		return fmt.Errorf("requiredClusterSelector not found")
	}

	rcsMap, ok := rcs.(map[string]interface{})
	if !ok {
		return fmt.Errorf("requiredClusterSelector is not a map")
	}

	ls, found := rcsMap["labelSelector"]
	if !found {
		return fmt.Errorf("labelSelector not found")
	}

	lsMap, ok := ls.(map[string]interface{})
	if !ok {
		return fmt.Errorf("labelSelector is not a map")
	}

	me, found := lsMap["matchExpressions"]
	if !found {
		return fmt.Errorf("matchExpressions not found")
	}

	meSlice, ok := me.([]interface{})
	if !ok || len(meSlice) == 0 {
		return fmt.Errorf("matchExpressions is not a valid slice")
	}

	expr, ok := meSlice[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid matchExpression structure")
	}

	expr["values"] = values
	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacement(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName, policyNamespace, placementName, desiredName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      placementName,
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
			"predicates": []interface{}{
				map[string]interface{}{
					"requiredClusterSelector": map[string]interface{}{
						"labelSelector": map[string]interface{}{
							"matchExpressions": []interface{}{
								map[string]interface{}{
									"key":      "name",
									"operator": "In",
									"values":   []interface{}{},
								},
							},
						},
					},
				},
			},
			"tolerations": []interface{}{
				map[string]interface{}{
					"key":      "cluster.open-cluster-management.io/unavailable",
					"operator": "Exists",
				},
				map[string]interface{}{
					"key":      "cluster.open-cluster-management.io/unreachable",
					"operator": "Exists",
				},
			},
		},
	}

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.open-cluster-management.io",
		Kind:    "Placement",
		Version: "v1beta1",
	})

	return u
}

// PolicyEvaluationDeps provides dependency injection for policy evaluation functions
type PolicyEvaluationDeps struct {
	GetPolicy     func(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error)
	GetCompliance func(clusterName string, policy *unstructured.Unstructured) string
	ShouldSoak    func(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error)
}

/*
getNextNonCompliantPolicyForCluster goes through all the policies in the managedPolicies list, starting with the

	policy index for the requested cluster and returns the index of the first policy that has the cluster as NonCompliant.

	returns: policyIndex the index of the next policy for which the cluster is NonCompliant or -1 if no policy found
	         error/nil
*/
func (r *ClusterGroupUpgradeReconciler) getNextNonCompliantPolicyForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int, deps *PolicyEvaluationDeps) (int, bool, error) {

	// Set up dependency functions - use injected dependencies or fallback to defaults
	getPolicy := r.getPolicyByName
	getCompliance := r.getClusterComplianceWithPolicy
	shouldSoak := utils.ShouldSoak

	if deps != nil {
		if deps.GetPolicy != nil {
			getPolicy = deps.GetPolicy
		}
		if deps.GetCompliance != nil {
			getCompliance = deps.GetCompliance
		}
		if deps.ShouldSoak != nil {
			shouldSoak = deps.ShouldSoak
		}
	}

	isSoaking := false
	numberOfPolicies := len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	currentPolicyIndex := startIndex
	for ; currentPolicyIndex < numberOfPolicies; currentPolicyIndex++ {
		// Get the name of the managed policy matching the current index.
		currentManagedPolicyInfo := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[currentPolicyIndex]
		currentManagedPolicy, err := getPolicy(ctx, currentManagedPolicyInfo.Name, currentManagedPolicyInfo.Namespace)
		if err != nil {
			return currentPolicyIndex, isSoaking, err
		}

		// Check if current cluster is compliant or not for its current managed policy.
		clusterStatus := getCompliance(clusterName, currentManagedPolicy)

		// If the cluster is compliant for the policy or if the cluster is not matched with the policy,
		// move to the next policy index.
		if clusterStatus == utils.ClusterNotMatchedWithPolicy {
			continue
		}

		// after all batches are finished, controller goes through all previous batches to see
		// if policies are still compliant; in this case some cluster will not be present in
		// CurrentBatchRemediationProgress and there is no need to check soaking or modify
		// FirstCompliantAt
		_, clusterInBatch := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]

		if clusterStatus == utils.ClusterStatusCompliant {
			if !clusterInBatch {
				continue
			}
			soakResult, err := shouldSoak(currentManagedPolicy, clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt)
			if err != nil {
				r.Log.Info(err.Error())
				continue
			}
			if !soakResult {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Time{}
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
			if clusterInBatch {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Time{}
			}
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
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, placementName string, managedPolicy *unstructured.Unstructured) error {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, managedPolicy.GetNamespace(), clusterGroupUpgrade, utils.MaxObjectNameLength)
	// Ensure batch placement bindings.
	pb := r.newBatchPlacementBinding(clusterGroupUpgrade, managedPolicy.GetName(), managedPolicy.GetNamespace(), placementName, safeName, name)

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err := r.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: managedPolicy.GetNamespace(),
	}, foundPlacementBinding)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, pb)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		pb.SetResourceVersion(foundPlacementBinding.GetResourceVersion())
		err = r.Update(ctx, pb)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	policyName, policyNamespace, placementName, placementBindingName, desiredName string) *unstructured.Unstructured {

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
			"name":     placementName,
			"kind":     "Placement",
			"apiGroup": "cluster.open-cluster-management.io",
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

func (r *ClusterGroupUpgradeReconciler) getPlacements(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName *string, policyNamespace string) (*unstructured.UnstructuredList, error) {
	var placementLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
	}
	if policyName != nil {
		placementLabels["openshift-cluster-group-upgrades/forPolicy"] = *policyName
	}

	listOpts := []client.ListOption{
		client.InNamespace(policyNamespace),
		client.MatchingLabels(placementLabels),
	}
	placementsList := &unstructured.UnstructuredList{}
	placementsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.open-cluster-management.io",
		Kind:    "PlacementList",
		Version: "v1beta1",
	})
	if err := r.List(ctx, placementsList, listOpts...); err != nil {
		return nil, err
	}

	return placementsList, nil
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
		placementName, err := r.ensureBatchPlacement(ctx, clusterGroupUpgrade, managedPolicy)
		if err != nil {
			return err
		}

		err = r.ensureBatchPlacementBinding(ctx, clusterGroupUpgrade, placementName, managedPolicy)
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
	  based on a policy's status structure which is below. If a policy is bound to a Placement, then
	  all the clusters bound to the policy will appear in status.status as either Compliant or NonCompliant.

	  status:
	    compliant: NonCompliant
	    placement:
	    - placementBinding: binding-policy1-common-cluster-version-policy
	      placement: placement-policy1-common-cluster-version-policy
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
			switch crtSubStatusMap["compliant"] {
			case utils.ClusterStatusCompliant:
				return utils.ClusterStatusCompliant
			case utils.ClusterStatusNonCompliant, utils.ClusterStatusPending:
				// Treat pending as non-compliant
				return utils.ClusterStatusNonCompliant
			case nil:
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
			nextNonCompliantPolicyIndex, isSoaking, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, batchClusterName, 0, nil)
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
				err := r.Delete(ctx, newResource)
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

	placementNames := make([]string, 0)
	placementBindingNames := make([]string, 0)
	for _, ns := range targetNamespaces {
		placements, err := r.getPlacements(ctx, clusterGroupUpgrade, nil, ns)
		if err != nil {
			return err
		}

		for _, placement := range placements.Items {
			placementNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, placementNames, &placement)
			if err != nil {
				return err
			}
		}
		clusterGroupUpgrade.Status.Placements = placementNames

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
		err := r.List(ctx, cgus)
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
