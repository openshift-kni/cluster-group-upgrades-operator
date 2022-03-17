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

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// templateData provides template rendering data
type templateData struct {
	Cluster               string
	ResourceName          string
	PlatformImage         string
	Operators             operatorsData
	WorkloadImage         string
	JobTimeout            uint64
	ViewUpdateIntervalSec int
}

// operatorsData provides operators data for template rendering
type operatorsData struct {
	Indexes             []string
	PackagesAndChannels []string
}

// resourceTemplate define a resource template structure
type resourceTemplate struct {
	// Must always correspond the Action or View resource name
	resourceName string
	// Template text
	template string
}

var precacheDependenciesCreateTemplates = []resourceTemplate{
	{"precache-ns-create", templates.MngClusterActCreatePrecachingNS},
	{"precache-spec-cm-create", templates.MngClusterActCreatePrecachingSpecCM},
	{"precache-sa-create", templates.MngClusterActCreateServiceAcct},
	{"precache-crb-create", templates.MngClusterActCreateClusterRoleBinding},
}

var precacheDependenciesViewTemplates = []resourceTemplate{
	{"view-precache-spec-configmap", templates.MngClusterViewConfigMap},
	{"view-precache-service-acct", templates.MngClusterViewServiceAcct},
	{"view-precache-cluster-role-binding", templates.MngClusterViewClusterRoleBinding},
}

var precacheCreateTemplates = []resourceTemplate{
	{"precache-job-create", templates.MngClusterActCreateJob},
	{"view-precache-job", templates.MngClusterViewJob},
}
var precacheJobView = []resourceTemplate{
	{"view-precache-job", templates.MngClusterViewJob},
}
var precacheDeleteTemplates = []resourceTemplate{
	{"precache-ns-delete", templates.MngClusterActDeletePrecachingNS},
}

var precacheNSViewTemplates = []resourceTemplate{
	{"view-precache-namespace", templates.MngClusterViewNamespace},
}

var allViews = []resourceTemplate{ // only used for deleting, hence empty templates
	{"view-precache-namespace", ""},
	{"view-precache-job", ""},
	{"view-precache-spec-configmap", ""},
	{"view-precache-service-acct", ""},
	{"view-precache-cluster-role-binding", ""},
}

// createResourceFromTemplate creates managedclusteraction or managedclusterview
//      resources from templates using dynamic client
// returns:   error
func (r *ClusterGroupUpgradeReconciler) createResourcesFromTemplates(
	ctx context.Context, data *templateData, templates []resourceTemplate) error {

	config := ctrl.GetConfigOrDie()
	dynamic := dynamic.NewForConfigOrDie(config)

	for _, item := range templates {
		r.Log.Info("[createResourcesFromTemplates]", "cluster", data.Cluster, "template", item.resourceName)
		obj := &unstructured.Unstructured{}
		w, err := r.renderYamlTemplate(item.resourceName, item.template, *data)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

		// decode YAML into unstructured.Unstructured
		dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		_, gvk, err := dec.Decode(w.Bytes(), nil, obj)
		if err != nil {
			return err
		}
		// Map GVK to GVR with discovery client
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
		if err != nil {
			return err
		}
		mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}
		// Build resource
		resource := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: mapping.Resource.Resource,
		}
		_, err = dynamic.Resource(resource).Namespace(data.Cluster).Create(ctx, obj, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				r.Log.Info("[createResourcesFromTemplates] Already exists",
					"cluster", data.Cluster, "template", item.resourceName)
				return nil
			}
			return err
		}
	}
	return nil
}

// deleteManagedClusterViewResource deletes view by name and namespace
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteManagedClusterViewResource(
	ctx context.Context, name string, namespace string) error {

	config := ctrl.GetConfigOrDie()
	dynamic := dynamic.NewForConfigOrDie(config)
	resourceID := schema.GroupVersionResource{
		Group:    "view.open-cluster-management.io",
		Version:  "v1beta1",
		Resource: "managedclusterviews",
	}
	err := dynamic.Resource(resourceID).Namespace(namespace).Delete(
		ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	return nil
}

// getView gets view resource
// returns:     *unstructured.Unstructured (view data)
//              bool (available)
//              error
func (r *ClusterGroupUpgradeReconciler) getView(
	ctx context.Context, name string, namespace string) (
	*unstructured.Unstructured, bool, error) {

	view := &unstructured.Unstructured{}
	view.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "view.open-cluster-management.io",
		Kind:    "ManagedClusterView",
		Version: "v1beta1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, view)
	if err != nil {
		if errors.IsNotFound(err) {
			return view, false, nil
		}
		return view, false, err
	}
	return view, true, nil
}

// deleteAllViews deletes all managed cluster view resources
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteAllViews(ctx context.Context, cluster string) error {
	// Cleanup all existing view objects that might have been left behind
	// in case of a crash etc.
	for _, item := range allViews {
		err := r.deleteManagedClusterViewResource(ctx, item.resourceName, cluster)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// checkViewProcessing checks whether managedclusterview is processing
// returns: 	processing bool
func (r *ClusterGroupUpgradeReconciler) checkViewProcessing(viewConditions []interface{}) bool {
	var status string
	for _, condition := range viewConditions {
		if condition.(map[string]interface{})["type"] == "Processing" {
			status = condition.(map[string]interface{})["status"].(string)
			message := condition.(map[string]interface{})["message"].(string)
			r.Log.Info("[checkViewProcessing]", "viewStatus", message)
			break
		}
	}
	return status == "True"
}

// renderYamlTemplate renders a single yaml template
//            resourceName - resource name
//            templateBody - template body
// returns:   bytes.Buffer rendered template
//            error
func (r *ClusterGroupUpgradeReconciler) renderYamlTemplate(
	resourceName string,
	templateBody string,
	data templateData) (*bytes.Buffer, error) {

	w := new(bytes.Buffer)
	template, err := template.New(resourceName).Parse(templates.CommonTemplates + templateBody)
	if err != nil {
		return w, fmt.Errorf("failed to parse template %s: %v", resourceName, err)
	}
	data.ResourceName = resourceName
	err = template.Execute(w, data)
	if err != nil {
		return w, fmt.Errorf("failed to render template %s: %v", resourceName, err)
	}
	return w, nil
}
