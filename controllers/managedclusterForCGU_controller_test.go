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

package controllers

import (
	"context"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	policiesv1 "github.com/open-cluster-management/governance-policy-propagator/api/v1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	testscheme = scheme.Scheme
)

func init() {
	testscheme.AddKnownTypes(clusterv1.GroupVersion, &clusterv1.ManagedCluster{})
	testscheme.AddKnownTypes(ranv1alpha1.GroupVersion, &ranv1alpha1.ClusterGroupUpgrade{})
	testscheme.AddKnownTypes(ranv1alpha1.GroupVersion, &ranv1alpha1.ClusterGroupUpgradeList{})
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.Policy{})
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.PolicyList{})
}

func getFakeClientFromObjects(objs ...client.Object) (client.WithWatch, error) {
	c := fake.NewClientBuilder().WithScheme(testscheme).WithObjects(objs...).Build()
	return c, nil
}

func TestControllerReconciler(t *testing.T) {
	testcases := []struct {
		name         string
		objs         []client.Object
		request      reconcile.Request
		validateFunc func(t *testing.T, result ctrl.Result, runtimeClient client.Client)
	}{
		{
			name: "no managed cluster",
			objs: []client.Object{},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
				if err := runtimeClient.Get(context.TODO(), types.NamespacedName{Name: "testSpoke", Namespace: "ztp-install"}, clusterGroupUpgrade); err == nil {
					t.Errorf("expected NotFound error, but failed")
				}
			},
		},
		{
			name: "managed cluster has no ready status",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				if result.IsZero() || result.RequeueAfter != clusterStatusCheckRetryDelay {
					t.Errorf("expect to reconcile after %v, but failed", clusterStatusCheckRetryDelay)
				}
			},
		},
		{
			name: "managed cluster is not ready",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
					Status: clusterv1.ManagedClusterStatus{
						Conditions: []v1.Condition{
							{
								Type:   clusterv1.ManagedClusterConditionAvailable,
								Status: v1.ConditionFalse,
							},
						},
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				if result.IsZero() || result.RequeueAfter != clusterStatusCheckRetryDelay {
					t.Errorf("expect to reconcile after %v, but failed", clusterStatusCheckRetryDelay)
				}
			},
		},
		{
			name: "managed cluster is ready but no child policies",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
					Status: clusterv1.ManagedClusterStatus{
						Conditions: []v1.Condition{
							{
								Type:   clusterv1.ManagedClusterConditionAvailable,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				if !result.IsZero() {
					t.Errorf("expect to stop reconcile, but failed")
				}
			},
		},
		{
			name: "managed cluster is ready but all found child policies have no waves",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
					Status: clusterv1.ManagedClusterStatus{
						Conditions: []v1.Condition{
							{
								Type:   clusterv1.ManagedClusterConditionAvailable,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:      "ztp-common.common-config-policy",
						Namespace: "testSpoke",
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:      "ztp-common.common-sub-policy",
						Namespace: "testSpoke",
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:      "ztp-group.group-du-config-policy",
						Namespace: "testSpoke",
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				if !result.IsZero() {
					t.Errorf("expect to stop reconcile, but failed")
				}
			},
		},
		{
			name: "managed cluster is ready and partial found child policies have no waves",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
					Status: clusterv1.ManagedClusterStatus{
						Conditions: []v1.Condition{
							{
								Type:   clusterv1.ManagedClusterConditionAvailable,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:        "ztp-common.common-config-policy",
						Namespace:   "testSpoke",
						Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "1"},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:        "ztp-common.common-sub-policy",
						Namespace:   "testSpoke",
						Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "20"},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:      "ztp-group.group-du-config-policy",
						Namespace: "testSpoke",
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
				if err := runtimeClient.Get(context.TODO(), types.NamespacedName{Name: "testSpoke", Namespace: "ztp-install"}, clusterGroupUpgrade); err != nil {
					if errors.IsNotFound(err) {
						t.Errorf("excepted one CGU created, but failed")
					}
					t.Errorf("unexcepted error: %v", err.Error())
				}

				assert.Equal(t, clusterGroupUpgrade.ObjectMeta.Name, "testSpoke")
				assert.Equal(t, clusterGroupUpgrade.ObjectMeta.Namespace, "ztp-install")
				assert.Equal(t, clusterGroupUpgrade.Spec.Enable, true)
				assert.Equal(t, clusterGroupUpgrade.Spec.Clusters, []string{"testSpoke"})
				assert.Equal(t, clusterGroupUpgrade.Spec.ManagedPolicies, []string{"common-config-policy", "common-sub-policy"})
				assert.Equal(t, clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency, 1)
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.BeforeEnable.AddClusterLabels, map[string]string{ztpRunningLabel: ""})
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.AfterCompletion.AddClusterLabels, map[string]string{ztpDoneLabel: ""})
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.AfterCompletion.DeleteClusterLabels, map[string]string{ztpRunningLabel: ""})

				if !result.IsZero() {
					t.Errorf("expect to stop reconcile, but failed")
				}
			},
		},
		{
			name: "managed cluster is ready and child policies are found",
			objs: []client.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: v1.ObjectMeta{
						Name: "testSpoke",
					},
					Status: clusterv1.ManagedClusterStatus{
						Conditions: []v1.Condition{
							{
								Type:   clusterv1.ManagedClusterConditionAvailable,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:        "ztp-common.common-config-policy",
						Namespace:   "testSpoke",
						Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "1"},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:        "ztp-common.common-sub-policy",
						Namespace:   "testSpoke",
						Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "2"},
					},
				},
				&policiesv1.Policy{
					ObjectMeta: v1.ObjectMeta{
						Name:        "ztp-group.group-du-config-policy",
						Namespace:   "testSpoke",
						Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "1000000000000000"},
					},
				},
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "testSpoke",
				},
			},
			validateFunc: func(t *testing.T, result ctrl.Result, runtimeClient client.Client) {
				clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
				if err := runtimeClient.Get(context.TODO(), types.NamespacedName{Name: "testSpoke", Namespace: "ztp-install"}, clusterGroupUpgrade); err != nil {
					if errors.IsNotFound(err) {
						t.Errorf("excepted one CGU created, but failed")
					}
					t.Errorf("unexcepted error: %v", err.Error())
				}

				assert.Equal(t, clusterGroupUpgrade.ObjectMeta.Name, "testSpoke")
				assert.Equal(t, clusterGroupUpgrade.ObjectMeta.Namespace, "ztp-install")
				assert.Equal(t, clusterGroupUpgrade.Spec.Enable, true)
				assert.Equal(t, clusterGroupUpgrade.Spec.Clusters, []string{"testSpoke"})
				assert.Equal(t, clusterGroupUpgrade.Spec.ManagedPolicies, []string{"common-config-policy", "common-sub-policy", "group-du-config-policy"})
				assert.Equal(t, clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency, 1)
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.BeforeEnable.AddClusterLabels, map[string]string{ztpRunningLabel: ""})
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.AfterCompletion.AddClusterLabels, map[string]string{ztpDoneLabel: ""})
				assert.Equal(t, clusterGroupUpgrade.Spec.Actions.AfterCompletion.DeleteClusterLabels, map[string]string{ztpRunningLabel: ""})

				if !result.IsZero() {
					t.Errorf("expect to stop reconcile, but failed")
				}
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ns := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "ztp-install",
				},
			}
			objs := append(tc.objs, ns)
			fakeClient, err := getFakeClientFromObjects(objs...)
			if err != nil {
				t.Errorf("error in creating fake client")
			}

			r := &ManagedClusterForCguReconciler{
				Client: fakeClient,
				Log:    logr.Discard(),
				Scheme: fakeClient.Scheme(),
			}
			result, err := r.Reconcile(context.TODO(), tc.request)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			tc.validateFunc(t, result, r.Client)
		})
	}
}

func TestControllerSortMapByValue(t *testing.T) {
	testcases := []struct {
		inputMap       map[string]int
		expectedResult []string
	}{
		{
			inputMap:       map[string]int{"abcdef": 20, "bcd": -1, "acd": 10, "efg": 2, "cdef": 100},
			expectedResult: []string{"bcd", "efg", "acd", "abcdef", "cdef"},
		},
		{
			inputMap:       map[string]int{"abcdef": 20, "bcd": -2, "acd": 8, "efg": 20, "cdef": 20},
			expectedResult: []string{"bcd", "acd", "abcdef", "cdef", "efg"},
		},
		{
			inputMap:       map[string]int{"abcdef": 20, "bcd": 8, "acd": 8, "efg": 20, "cdef": 1},
			expectedResult: []string{"cdef", "acd", "bcd", "abcdef", "efg"},
		},
		{
			inputMap:       map[string]int{"abcdef": 20, "bcd": 8, "acd": 0, "abcdehhh": 20, "cdef": 8},
			expectedResult: []string{"acd", "bcd", "cdef", "abcdef", "abcdehhh"},
		},
	}

	for _, tc := range testcases {
		result := sortMapByValue(tc.inputMap)
		assert.Equal(t, result, tc.expectedResult)
	}
}

func TestControllerReconcileWithHundredClusters(t *testing.T) {
	var objs []client.Object
	var requests []reconcile.Request

	ns := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "ztp-install",
		},
	}
	objs = append(objs, ns)

	for i := 1; i <= 100; i++ {
		name := "spoke" + strconv.Itoa(i)
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: v1.ObjectMeta{
				Name: name,
			},
			Status: clusterv1.ManagedClusterStatus{
				Conditions: []v1.Condition{
					{
						Type:   clusterv1.ManagedClusterConditionAvailable,
						Status: v1.ConditionTrue,
					},
				},
			},
		}
		objs = append(objs, cluster)

		policy := &policiesv1.Policy{
			ObjectMeta: v1.ObjectMeta{
				Name:        "ztp-common.common-config-policy",
				Namespace:   name,
				Annotations: map[string]string{"ran.openshift.io/ztp-deploy-wave": "1"},
			},
		}
		objs = append(objs, policy)

		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: name,
			},
		}
		requests = append(requests, request)
	}

	fakeClient, err := getFakeClientFromObjects(objs...)
	if err != nil {
		t.Errorf("error in creating fake client")
	}

	r := &ManagedClusterForCguReconciler{
		Client: fakeClient,
		Log:    logr.Discard(),
		Scheme: fakeClient.Scheme(),
	}
	for _, request := range requests {
		_, err := r.Reconcile(context.TODO(), request)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	clusterGroupUpgrades := &ranv1alpha1.ClusterGroupUpgradeList{}
	if err := r.Client.List(context.TODO(), clusterGroupUpgrades, client.InNamespace("ztp-install")); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(clusterGroupUpgrades.Items) != 100 {
		t.Errorf("expected a hundred of ClusterGroupUpgrades, but failed with %d", len(clusterGroupUpgrades.Items))
	}
}
