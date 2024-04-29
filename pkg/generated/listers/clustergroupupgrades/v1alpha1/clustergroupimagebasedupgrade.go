/*
Copyright 2021.

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
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ClusterGroupImageBasedUpgradeLister helps list ClusterGroupImageBasedUpgrades.
// All objects returned here must be treated as read-only.
type ClusterGroupImageBasedUpgradeLister interface {
	// List lists all ClusterGroupImageBasedUpgrades in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ClusterGroupImageBasedUpgrade, err error)
	// ClusterGroupImageBasedUpgrades returns an object that can list and get ClusterGroupImageBasedUpgrades.
	ClusterGroupImageBasedUpgrades(namespace string) ClusterGroupImageBasedUpgradeNamespaceLister
	ClusterGroupImageBasedUpgradeListerExpansion
}

// clusterGroupImageBasedUpgradeLister implements the ClusterGroupImageBasedUpgradeLister interface.
type clusterGroupImageBasedUpgradeLister struct {
	indexer cache.Indexer
}

// NewClusterGroupImageBasedUpgradeLister returns a new ClusterGroupImageBasedUpgradeLister.
func NewClusterGroupImageBasedUpgradeLister(indexer cache.Indexer) ClusterGroupImageBasedUpgradeLister {
	return &clusterGroupImageBasedUpgradeLister{indexer: indexer}
}

// List lists all ClusterGroupImageBasedUpgrades in the indexer.
func (s *clusterGroupImageBasedUpgradeLister) List(selector labels.Selector) (ret []*v1alpha1.ClusterGroupImageBasedUpgrade, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ClusterGroupImageBasedUpgrade))
	})
	return ret, err
}

// ClusterGroupImageBasedUpgrades returns an object that can list and get ClusterGroupImageBasedUpgrades.
func (s *clusterGroupImageBasedUpgradeLister) ClusterGroupImageBasedUpgrades(namespace string) ClusterGroupImageBasedUpgradeNamespaceLister {
	return clusterGroupImageBasedUpgradeNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ClusterGroupImageBasedUpgradeNamespaceLister helps list and get ClusterGroupImageBasedUpgrades.
// All objects returned here must be treated as read-only.
type ClusterGroupImageBasedUpgradeNamespaceLister interface {
	// List lists all ClusterGroupImageBasedUpgrades in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ClusterGroupImageBasedUpgrade, err error)
	// Get retrieves the ClusterGroupImageBasedUpgrade from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ClusterGroupImageBasedUpgrade, error)
	ClusterGroupImageBasedUpgradeNamespaceListerExpansion
}

// clusterGroupImageBasedUpgradeNamespaceLister implements the ClusterGroupImageBasedUpgradeNamespaceLister
// interface.
type clusterGroupImageBasedUpgradeNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ClusterGroupImageBasedUpgrades in the indexer for a given namespace.
func (s clusterGroupImageBasedUpgradeNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.ClusterGroupImageBasedUpgrade, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ClusterGroupImageBasedUpgrade))
	})
	return ret, err
}

// Get retrieves the ClusterGroupImageBasedUpgrade from the indexer for a given namespace and name.
func (s clusterGroupImageBasedUpgradeNamespaceLister) Get(name string) (*v1alpha1.ClusterGroupImageBasedUpgrade, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("clustergroupimagebasedupgrade"), name)
	}
	return obj.(*v1alpha1.ClusterGroupImageBasedUpgrade), nil
}
