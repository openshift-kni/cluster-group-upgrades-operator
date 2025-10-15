package utils

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicyErr type
type PolicyErr struct {
	ObjName string
	ErrMsg  string
}

func (e *PolicyErr) Error() string {
	return fmt.Sprintf("%s: %s", e.ObjName, e.ErrMsg)
}

// StringToYaml takes a string and attempts to unmarshal it into a YAML
func StringToYaml(s string) (interface{}, error) {
	var yamlObj interface{}
	if err := yaml.Unmarshal([]byte(s), &yamlObj); err != nil {
		return yamlObj, fmt.Errorf("could not unmarshal data: %s", err)
	}
	return yamlObj, nil
}

// GetChildPolicies gets the child policies for a list of clusters
func GetChildPolicies(ctx context.Context, c client.Client, clusters []string) ([]policiesv1.Policy, error) {
	var childPolicies []policiesv1.Policy

	for _, clusterName := range clusters {
		policies := &policiesv1.PolicyList{}
		if err := c.List(ctx, policies, client.InNamespace(clusterName)); err != nil {
			return nil, err
		}

		for _, policy := range policies.Items {
			labels := policy.GetLabels()
			if labels == nil {
				continue
			}
			// Skip if it's the child policy of a copied policy.
			if _, ok := labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"]; ok {
				continue
			}
			// If we can find the child policy specific label, add the child policy name to the list.
			if _, ok := labels[ChildPolicyLabel]; ok {
				childPolicies = append(childPolicies, policy)
			}
		}
	}

	return childPolicies, nil
}

// DeletePolicies deletes Policies
func DeletePolicies(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}
	policiesList := &policiesv1.PolicyList{}
	if err := c.List(ctx, policiesList, listOpts...); err != nil {
		return err
	}

	for _, policy := range policiesList.Items {
		if err := c.Delete(ctx, &policy); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// DeletePlacementBindings deletes PlacementBindings
func DeletePlacementBindings(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	deleteAllOpts := []client.DeleteAllOfOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}
	placementBinding := &unstructured.Unstructured{}
	placementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	if err := c.DeleteAllOf(ctx, placementBinding, deleteAllOpts...); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

// DeletePlacementRules deletes PlacementRules
func DeletePlacementRules(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	deleteAllOpts := []client.DeleteAllOfOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}

	placementRule := &unstructured.Unstructured{}
	placementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	if err := c.DeleteAllOf(ctx, placementRule, deleteAllOpts...); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

// GetResourceName constructs composite names for policy objects
func GetResourceName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, initialString string) string {
	return strings.ToLower(clusterGroupUpgrade.Name + "-" + initialString)
}

// GetParentPolicyNameAndNamespace gets the parent policy name and namespace from a given child policy
// returns: []string       a two-element slice which the first element is policy namespace and the second one is policy name
func GetParentPolicyNameAndNamespace(childPolicyName string) ([]string, error) {
	// The format of a child policy name is parent_policy_namespace.parent_policy_name.
	// Extract the parent policy name and namespace by splitting the child policy name into two substrings separated by "."
	// and we are safe to split with the separator "." as the namespace is disallowed to contain "."
	res := strings.SplitN(childPolicyName, ".", 2)
	if len(res) != 2 {
		return nil, errors.New("child policy name " + childPolicyName + " is not valid.")
	}
	return res, nil
}

// InspectPolicyObjects validates the policy objects, checks if it contains a status section in any object templates
// and return error if the policy is invalid
func InspectPolicyObjects(policy *unstructured.Unstructured) (bool, error) {

	containsStatus := false
	policyName := policy.GetName()
	policySpec := policy.Object["spec"].(map[string]interface{})

	// Get the policy templates.
	policyTemplates := policySpec["policy-templates"].([]interface{})

	// Go through the policy policy-templates.
	for _, plcTmpl := range policyTemplates {
		// Make sure the objectDefinition of the policy template exists.
		if plcTmpl.(map[string]interface{})["objectDefinition"] == nil {
			return containsStatus, &PolicyErr{policyName, PlcMissTmplDef}
		}
		plcTmplDef := plcTmpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})

		// Make sure the ConfigurationPolicy metadata exists.
		if plcTmplDef["metadata"] == nil {
			return containsStatus, &PolicyErr{policyName, PlcMissTmplDefMeta}
		}

		// Make sure the ConfigurationPolicy spec exists.
		if plcTmplDef["spec"] == nil {
			return containsStatus, &PolicyErr{policyName, PlcMissTmplDefSpec}
		}
		plcTmplDefSpec := plcTmplDef["spec"].(map[string]interface{})

		// One and only one of [object-templates, object-templates-raw] should be defined
		objectTemplatePresent := plcTmplDefSpec[ObjectTemplates] != nil
		objectTemplateRawPresent := plcTmplDefSpec[ObjectTemplatesRaw] != nil

		var configPlcTmpls interface{}

		switch {
		case objectTemplatePresent && objectTemplateRawPresent:
			return containsStatus, &PolicyErr{policyName, ConfigPlcHasBothObjTmpl}
		case objectTemplatePresent:
			configPlcTmpls = plcTmplDefSpec[ObjectTemplates].([]interface{})
		case objectTemplateRawPresent:
			stringTemplate := StripObjectTemplatesRaw(plcTmplDefSpec[ObjectTemplatesRaw].(string))

			var err error
			configPlcTmpls, err = StringToYaml(stringTemplate)
			if err != nil {
				return containsStatus, &PolicyErr{policyName, ConfigPlcFailRawMarshal}
			}

		default:
			return containsStatus, &PolicyErr{policyName, ConfigPlcMissAnyObjTmpl}
		}

		// Go through the ConfigurationPolicy object-templates.
		for _, configPlcTmpl := range configPlcTmpls.([]interface{}) {
			// Make sure the objectDefinition of the ConfigurationPolicy object template exists.
			if configPlcTmpl.(map[string]interface{})["objectDefinition"] == nil {
				// No objectDefinition in the policy template, skipping.
				if objectTemplateRawPresent {
					continue
				} else {
					return containsStatus, &PolicyErr{policyName, ConfigPlcMissObjTmplDef}
				}
			}
			objectDefinition := configPlcTmpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			if objectDefinition["status"] != nil {
				containsStatus = true
			}
		}
	}
	return containsStatus, nil
}

// ShouldSoak returns whether the reconciler should wait for some time before moving on from a policy after it is compliant
func ShouldSoak(policy *unstructured.Unstructured, firstCompliantAt metav1.Time) (bool, error) {
	soak, ok := policy.GetAnnotations()[SoakAnnotation]
	if !ok {
		return false, nil
	}
	soakSeconds, err := strconv.Atoi(soak)
	if err != nil || soakSeconds < 0 {
		return false, errors.New("soak annotation value " + soak + " is invalid, value should be an integer equal or greater than 0")
	}

	if firstCompliantAt.IsZero() {
		return true, nil
	}
	if time.Since(firstCompliantAt.Time) > time.Duration(soakSeconds)*time.Second {
		return false, nil
	}
	return true, nil
}

// UpdateManagedPolicyNamespaceList updates policyNs with the corresponding namespaces of a managed policy
// as contained in the policyNameArr parameter.
func UpdateManagedPolicyNamespaceList(policyNs map[string][]string, policyNameArr []string) {
	crtPolicyName := policyNameArr[1]
	crtPolicyNs := policyNameArr[0]

	// If the current managed policy namespace doesn't exist in the namespace list, add it.
	nsFound := false
	for _, nsEntry := range policyNs[crtPolicyName] {
		if crtPolicyNs == nsEntry {
			nsFound = true
			break
		}
	}
	if !nsFound {
		policyNs[crtPolicyName] = append(policyNs[crtPolicyName], crtPolicyNs)
	}
}

// StripObjectTemplatesRaw removes all the ACM raw templating from a string and returns an interface
// of what the object-templates would be if not for the raw templating
func StripObjectTemplatesRaw(tmplStr string) string {

	// Create a copy of the input since we will be editing it multiple times
	result := tmplStr

	// Get all variable usages
	variableUsageRegex := regexp.MustCompile(`{{\s*\$[\w\.]*\s*}}`)
	variableUsages := variableUsageRegex.FindAllStringSubmatch(result, -1)

	// Get the usage of all of those variables
	for _, item := range variableUsages {
		// item[0] is the full usage of a variable
		// e.g. "{{ $my.example.var }}"

		// Replace all plain usages of variable with placeholder
		result = strings.ReplaceAll(result, item[0], Placeholder)
	}

	// Get all inline usage of declared ranges
	inlineUsageRegex := regexp.MustCompile(`{{\s*\.[\w\.]*\s*}}`)
	inlineUsages := inlineUsageRegex.FindAllStringSubmatch(result, -1)

	// Get the usage of all of those inlines
	for _, item := range inlineUsages {
		// item[0] is the full usage of a inline
		// e.g. "{{ .data.item }}"

		// Replace all plain usages of inlines with placeholder
		result = strings.ReplaceAll(result, item[0], Placeholder)
	}

	// Get everything between sets of brackets as substrings
	bracketRegex := regexp.MustCompile(`\{{[^}]+}\}`)

	// Each result here will be a substring including the start and end brackets
	// e.g. "{{ $example.var.usage }}"
	bracketSubstrings := bracketRegex.FindAllStringSubmatch(result, -1)

	// For our usage all our results will be an array with a single item
	// so we will just use item[0] here
	for _, item := range bracketSubstrings {
		// We want to remove all the ACM templates and the hub side templates
		// Deletes the template line entirely, including the newline at the end
		result = strings.Replace(result+"\n", item[0], "", 1)
	}

	return result
}
