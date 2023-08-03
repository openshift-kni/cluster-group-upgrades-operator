package utils

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TemplateResolver contains information for processing hub templates.
type TemplateResolver struct {
	client.Client
	Ctx             context.Context
	TargetNamespace string
	PolicyName      string
	PolicyNamespace string
}

var (
	hubTmplLog = ctrl.Log.WithName("utils.policy_template")

	// This regular expression is to extract all hub template functions from a string.
	regexpHubTmplFunc = regexp.MustCompile(`{{hub.*?(fromConfigMap|fromSecret|lookup).*?hub}}`)

	// This regular expression is to get the function name, resource name and namespace referenced in the function from a hub template.
	// The following captured groups represent for:
	//      $0: hub template
	// 		$1: any characters before the template function
	// 		$2: template function
	// 		$3: resource namespace field
	// 		$4: resource namespace with printf variable
	// 		$5: resource namespace with a fixed string
	// 		$6: resource name field
	// 		$7: resource name with printf variable
	// 		$8: resource name with a fixed string
	// 		$9: any characters after the resource name field
	regexpFromConfigMap = regexp.MustCompile(`({{hub.*?)(fromConfigMap)\s+((\(\s*printf\s.+?\s*\))|"(.*?)")\s+((\(\s*printf\s.+?\s*\))|"(.*?)")(.*?hub}})`)

	// This regular expression is to capture the lookup hub template function.
	// The following captured groups represent for:
	//      $0: hub template
	//      $1: resource api version
	//      $2: resource kind
	//      $3: resource namespace
	regexpLookup = regexp.MustCompile(`{{hub.*lookup\s+"(.*?)"\s+"(.*?)"\s+"(.*?)".*hub}}`)

	// This expression matches all types templates
	regexpAllTemplates = regexp.MustCompile(`{{.*}}`)
)

// ContainsTemplates checks if the string contains some templatized parts
func ContainsTemplates(s string) bool {
	return regexpAllTemplates.MatchString(s)
}

func stringToYaml(s string) (interface{}, error) {
	var yamlObj interface{}
	if err := yaml.Unmarshal([]byte(s), &yamlObj); err != nil {
		return yamlObj, fmt.Errorf("Could not unmarshal data: %s", err)
	}
	return yamlObj, nil
}

func yamlToString(y interface{}) (string, error) {
	b, err := yaml.Marshal(y)
	if err != nil {
		return "", fmt.Errorf("Could not marshal data: %s", err)
	}
	return string(b), nil
}

// VerifyHubTemplateFunctions validates any hub template function discovered in the object and
// return error if the template is not supported
func VerifyHubTemplateFunctions(tmpl interface{}, policyName string) error {
	tmplStr, err := yamlToString(tmpl)
	if err != nil {
		return err
	}

	hubTmplMatches := regexpHubTmplFunc.FindAllStringSubmatch(tmplStr, -1)
	if len(hubTmplMatches) == 0 {
		// No hub template functions found, return
		return nil
	}

	for _, hubTmplMatch := range hubTmplMatches {
		hubTmpl := hubTmplMatch[0]
		hubTmplFunc := hubTmplMatch[1]

		hubTmplLog.Info("Validating hub template in policy", "policy", policyName, "template", hubTmpl)
		if hubTmplFunc == "fromConfigMap" {
			matches := regexpFromConfigMap.FindStringSubmatch(hubTmpl)

			// Hub template doesn't match the regular expression
			if len(matches) == 0 {
				return &PolicyErr{hubTmpl, PlcHubTmplFmtErr}
			}

			if matches[4] != "" {
				return &PolicyErr{matches[4], PlcHubTmplPrinfInNsErr}
			}

			if matches[7] != "" {
				return &PolicyErr{matches[7], PlcHubTmplPrinfInNameErr}
			}
		} else if hubTmplFunc == "lookup" {
			matches := regexpLookup.FindStringSubmatch(hubTmpl)

			// Hub template doesn't match the regular expression
			if len(matches) == 0 {
				return &PolicyErr{hubTmpl, PlcHubTmplFmtErr}
			}

			if !strings.HasPrefix(matches[1], "cluster.open-cluster-management.io/") || matches[2] != "ManagedCluster" || matches[3] != "" {
				return &PolicyErr{hubTmplFunc, PlcLookupFuncResErr}
			}
		} else {
			return &PolicyErr{hubTmplFunc, PlcHubTmplFuncErr}
		}
	}

	return nil
}

// ProcessHubTemplateFunctions replicates any supported hub template resources discovered in the object
// to the CGU namespace and return the resolved template object
func (r *TemplateResolver) ProcessHubTemplateFunctions(tmpl interface{}) (interface{}, error) {
	tmplStr, err := yamlToString(tmpl)
	if err != nil {
		return tmpl, err
	}

	hubTmplMatches := regexpFromConfigMap.FindAllStringSubmatch(tmplStr, -1)
	if len(hubTmplMatches) == 0 {
		// No fromConfigMap hub functions found, skip processing
		return tmpl, nil
	}

	resolvedTmplStr := tmplStr
	for _, hubTmplMatch := range hubTmplMatches {
		hubTmpl := hubTmplMatch[0]
		namespace := hubTmplMatch[5]
		name := hubTmplMatch[8]

		hubTmplLog.Info("Processing hub template in policy", "policy", r.PolicyName, "template", hubTmpl)
		fromNamespace := namespace
		if namespace == "" {
			// namespace is empty
			fromNamespace = r.PolicyNamespace
		}

		fromResource := types.NamespacedName{
			Name:      name,
			Namespace: fromNamespace,
		}

		toResource := types.NamespacedName{
			Name:      r.PolicyNamespace + "." + name,
			Namespace: r.TargetNamespace,
		}

		if err := r.copyConfigmap(r.Ctx, fromResource, toResource); err != nil {
			return tmpl, err
		}

		// Update the hub templating with the replicated configmap name and namespace
		updatedHubTmpl := regexpFromConfigMap.ReplaceAllString(hubTmpl, `$1$2`+` "`+toResource.Namespace+`"`+` "`+toResource.Name+`"`+`$9`)
		resolvedTmplStr = strings.ReplaceAll(resolvedTmplStr, hubTmpl, updatedHubTmpl)
		hubTmplLog.Info("Processed hub template in policy", "policy", r.PolicyName, "template", updatedHubTmpl)
	}

	var resolvedTmpl interface{}
	resolvedTmpl, err = stringToYaml(resolvedTmplStr)
	if err != nil {
		return resolvedTmpl, err
	}

	return resolvedTmpl, nil
}

//nolint:gocritic
func (r *TemplateResolver) copyConfigmap(ctx context.Context, fromResource, toResource types.NamespacedName) error {
	// Get the original configmap referenced in the inform policy
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, fromResource, cm)
	if err != nil {
		return err
	}

	// Do not copy labels from the original configmap to avoid the copied configmap be deleted by ArgoCD
	copiedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toResource.Name,
			Namespace: toResource.Namespace,
		},
		Data:       cm.Data,
		BinaryData: cm.BinaryData,
		Immutable:  cm.Immutable,
	}

	annotations := cm.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[DesiredResourceName] = fromResource.Namespace + "." + fromResource.Name
	copiedCM.SetAnnotations(annotations)

	existingCM := &corev1.ConfigMap{}
	if err = r.Get(ctx, toResource, existingCM); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		if err := r.Create(ctx, copiedCM); err != nil {
			if !errors.IsAlreadyExists(err) {
				hubTmplLog.Error(err, "Fail to create config map", "name", copiedCM.Name, "namespace", copiedCM.Namespace)
				return err
			}
		}
	} else {
		err = r.Update(ctx, copiedCM)
		if err != nil {
			hubTmplLog.Error(err, "Fail to update config map", "name", copiedCM.Name, "namespace", copiedCM.Namespace)
			return err
		}
	}
	return nil
}
