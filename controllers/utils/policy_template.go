package utils

import (
	"context"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TemplateResolver contains information for processing hub templates.
type TemplateResolver struct {
	client.Client
	Ctx             context.Context
	Log             logr.Logger
	TargetNamespace string
	PolicyName      string
	PolicyNamespace string
}

// ResolveObjectHubTemplates searches from the configuration policy object template for all string values
// that contain hub templates and triggers template resource replication for each finding.
// returns: the updated policy object template
//
//	error/nil
//
//nolint:gocritic
func (r *TemplateResolver) ResolveObjectHubTemplates(objectDef interface{}) (interface{}, error) {

	var err error
	if objMap, isMap := objectDef.(map[string]interface{}); isMap {
		for key, value := range objMap {
			if objMap[key], err = r.ResolveObjectHubTemplates(value); err != nil {
				return objectDef, err
			}
		}
	} else if objSlice, isSlice := objectDef.([]interface{}); isSlice {
		for key, value := range objSlice {
			if objSlice[key], err = r.ResolveObjectHubTemplates(value); err != nil {
				return objectDef, err
			}
		}
	} else if objString, isString := objectDef.(string); isString {
		if strings.Contains(objString, "{{hub") {
			r.Log.Info("Found hub template in policy", "policy", r.PolicyName, "template", objString)
			if objectDef, err = r.replicateHubTemplateResource(objString); err != nil {
				r.Log.Error(err, "Failed to resolve hub template")
				return objectDef, err
			}
		}
	}

	return objectDef, nil
}

// ReplicateHubTemplateResource processes the hub templates string to identify the template function and
// template resource, then copies the template resource to the CGU namespace and updates the hub templates
// with replicated resource if conditions are met.
// returns:  the updated template string
//
//	error/nil
func (r *TemplateResolver) replicateHubTemplateResource(templates string) (string, error) {
	// This regular expression is to extract all hub templates from a string.
	re1 := regexp.MustCompile(`{{hub\s+.*?\s+hub}}`)

	// This regular expression is to get the function name, resource name and namespace referenced in the function from a hub template.
	// The following captured groups represent for:
	// 		$1: any characters before the template function
	// 		$2: template function
	// 		$3: resource namespace field
	// 		$4: resource namespace with printf variable
	// 		$5: resource namespace with a fixed string
	// 		$6: resource name field
	// 		$7: resource name with printf variable
	// 		$8: resource name with a fixed string
	// 		$9: any characters after the resource name field
	re2 := regexp.MustCompile(`({{hub.*)(fromConfigMap|fromSecret|lookup)\s+((\(\s*printf\s.+?\s*\))|"(.*?)")\s+((\(\s*printf\s.+?\s*\))|"(.*?)")(.*hub}})`)

	var resolvedTemplates = templates
	// Get all hub templates appeared in a string
	discoveredTemplates := re1.FindAllString(templates, -1)
	// Process each hub template
	for _, template := range discoveredTemplates {
		matches := re2.FindAllStringSubmatch(template, -1)

		// Hub template doesn't match the regular expression
		if len(matches) == 0 {
			return "", &PolicyErr{template, PlcHubTmplFmtErr}
		}

		for _, match := range matches {
			function := match[2]
			if function != "fromConfigMap" {
				return "", &PolicyErr{function, PlcHubTmplFuncErr}
			}

			if match[4] != "" {
				return "", &PolicyErr{match[4], PlcHubTmplPrinfInNsErr}
			}
			namespace := match[5]

			if match[7] != "" {
				return "", &PolicyErr{match[7], PlcHubTmplPrinfInNameErr}
			}
			name := match[8]

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
				return "", err
			}

			// Update the hub templating with the replicated configmap name and namespace
			updatedTemplate := re2.ReplaceAllString(template, `$1$2`+` "`+toResource.Namespace+`"`+` "`+toResource.Name+`"`+`$9`)
			resolvedTemplates = strings.ReplaceAll(resolvedTemplates, template, updatedTemplate)
		}
	}

	return resolvedTemplates, nil
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
				r.Log.Error(err, "Fail to create config map", "name", copiedCM.Name, "namespace", copiedCM.Namespace)
				return err
			}
		}
	} else {
		err = r.Update(ctx, copiedCM)
		if err != nil {
			r.Log.Error(err, "Fail to update config map", "name", copiedCM.Name, "namespace", copiedCM.Namespace)
			return err
		}
	}
	return nil
}
