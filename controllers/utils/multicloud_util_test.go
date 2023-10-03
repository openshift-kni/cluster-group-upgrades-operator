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

package utils

import (
	"context"
	"fmt"
	"testing"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	actionv1beta1 "github.com/stolostron/cluster-lifecycle-api/action/v1beta1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	testscheme = scheme.Scheme
)

func init() {
	testscheme.AddKnownTypes(ranv1alpha1.GroupVersion, &ranv1alpha1.ClusterGroupUpgrade{})
	testscheme.AddKnownTypes(ranv1alpha1.GroupVersion, &ranv1alpha1.ClusterGroupUpgradeList{})
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.Policy{})
	testscheme.AddKnownTypes(policiesv1.GroupVersion, &policiesv1.PolicyList{})
	testscheme.AddKnownTypes(actionv1beta1.GroupVersion, &actionv1beta1.ManagedClusterAction{})
	testscheme.AddKnownTypes(actionv1beta1.GroupVersion, &actionv1beta1.ManagedClusterActionList{})
	testscheme.AddKnownTypes(viewv1beta1.GroupVersion, &viewv1beta1.ManagedClusterView{})
	testscheme.AddKnownTypes(viewv1beta1.GroupVersion, &viewv1beta1.ManagedClusterViewList{})
	testscheme.AddKnownTypes(operatorsv1alpha1.SchemeGroupVersion, &operatorsv1alpha1.Subscription{})
	testscheme.AddKnownTypes(operatorsv1alpha1.SchemeGroupVersion, &operatorsv1alpha1.InstallPlan{})
}

func getFakeClientFromObjects(objs ...client.Object) (client.WithWatch, error) {
	c := fake.NewClientBuilder().WithScheme(testscheme).WithObjects(objs...).Build()
	return c, nil
}

func TestMultiCloudNewManagedClusterActionForInstallPlanSpec(t *testing.T) {
	testcases := []struct {
		name         string
		installPlan  operatorsv1alpha1.InstallPlan
		validateFunc func(t *testing.T, installPlan operatorsv1alpha1.InstallPlan)
	}{
		{
			name: "ManagedClusterAction is missing",
			installPlan: operatorsv1alpha1.InstallPlan{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-abcd", Namespace: "installPlan-abcd-namespace",
				},
				Spec: operatorsv1alpha1.InstallPlanSpec{
					Approval:                   "Manual",
					Approved:                   false,
					ClusterServiceVersionNames: []string{"ptp-operator.4.9.0-202201210133"},
				},
			},
			validateFunc: func(t *testing.T, installPlan operatorsv1alpha1.InstallPlan) {
				actionSpec, err := NewManagedClusterActionForInstallPlanSpec(installPlan)

				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}

				assert.Equal(t, actionSpec.KubeWork.Resource, "installplan")
				assert.Equal(t, actionSpec.KubeWork.Namespace, "installPlan-abcd-namespace")
				assert.Equal(t, actionSpec.ActionType, actionv1beta1.UpdateActionType)
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			objs = append(objs, &tc.installPlan)
			tc.validateFunc(t, tc.installPlan)
		})
	}
}

func TestEnsureManagedClusterActionForInstallPlan(t *testing.T) {
	testcases := []struct {
		name         string
		mca          *actionv1beta1.ManagedClusterAction
		mcaNamespace string
		cguLabel     string
		installPlan  operatorsv1alpha1.InstallPlan
		validateFunc func(t *testing.T, runtimeClient client.Client, mcaNamespace, cguLabel string, installPlan operatorsv1alpha1.InstallPlan)
	}{
		{
			name:         "ManagedClusterAction is successfully created",
			mca:          nil,
			mcaNamespace: "mcaNamespace",
			cguLabel:     "default-cgu",
			installPlan: operatorsv1alpha1.InstallPlan{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-abcd", Namespace: "installPlan-abcd-namespace",
				},
				Spec: operatorsv1alpha1.InstallPlanSpec{
					Approval:                   "Manual",
					Approved:                   false,
					ClusterServiceVersionNames: []string{"ptp-operator.4.9.0-202201210133"},
				},
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, mcaNamespace, cguLabel string, installPlan operatorsv1alpha1.InstallPlan) {
				mca, err := EnsureManagedClusterActionForInstallPlan(context.TODO(), runtimeClient, mcaNamespace, cguLabel, installPlan)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, mca.ObjectMeta.Name, installPlan.Name)
				assert.Equal(t, mca.ObjectMeta.Namespace, mcaNamespace)
				assert.Equal(t, mca.Spec.ActionType, actionv1beta1.UpdateActionType)
				assert.Equal(t, mca.Spec.KubeWork.Resource, "installplan")
				assert.Equal(t, mca.Spec.KubeWork.Namespace, "installPlan-abcd-namespace")
			},
		},
		{
			name: "ManagedClusterAction is missing condition indefinitely",
			mca: &actionv1beta1.ManagedClusterAction{
				ObjectMeta: v1.ObjectMeta{
					Name: "mcaName", Namespace: "mcaNamespace",
				},
				Spec: actionv1beta1.ActionSpec{
					ActionType: actionv1beta1.UpdateActionType,
					KubeWork: &actionv1beta1.KubeWorkSpec{
						Resource:  "installplan",
						Namespace: "installPlan-abcd-namespace",
					},
				},
			},
			mcaNamespace: "mcaNamespace",
			cguLabel:     "default-cgu",
			installPlan: operatorsv1alpha1.InstallPlan{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-abcd", Namespace: "installPlan-abcd-namespace",
				},
				Spec: operatorsv1alpha1.InstallPlanSpec{
					Approval:                   "Manual",
					Approved:                   false,
					ClusterServiceVersionNames: []string{"ptp-operator.4.9.0-202201210133"},
				},
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, mcaNamespace, cguLabel string, installPlan operatorsv1alpha1.InstallPlan) {
				mca, err := EnsureManagedClusterActionForInstallPlan(context.TODO(), runtimeClient, mcaNamespace, cguLabel, installPlan)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, mca.ObjectMeta.Name, installPlan.Name)
				assert.Equal(t, mca.ObjectMeta.Namespace, mcaNamespace)
				assert.Equal(t, mca.Spec.ActionType, actionv1beta1.UpdateActionType)
				assert.Equal(t, mca.Spec.KubeWork.Resource, "installplan")
				assert.Equal(t, mca.Spec.KubeWork.Namespace, "installPlan-abcd-namespace")
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			if tc.mca != nil {
				objs = append(objs, tc.mca)
			}
			objs = append(objs, &tc.installPlan)
			fakeClient, err := getFakeClientFromObjects(objs...)

			if err != nil {
				t.Errorf("error in creating fake client")
			}

			tc.validateFunc(t, fakeClient, tc.mcaNamespace, tc.cguLabel, tc.installPlan)
		})
	}
}

func TestEnsureManagedClusterView(t *testing.T) {
	testcases := []struct {
		name              string
		mcv               *viewv1beta1.ManagedClusterView
		mcvName           string
		safeMcvName       string
		mcvNamespace      string
		resourceType      string
		resourceName      string
		resourceNamespace string
		cguName           string
		cguNamespace      string
		validateFunc      func(t *testing.T, runtimeClient client.Client, safeMcvName, mcvName, mcvNamespace,
			resourceType, resourceName, resourceNamespace, cguName, cguNamespace string)
	}{
		{
			name:              "ManagedClusterView is successfully created",
			mcvName:           "view1",
			safeMcvName:       "view1-abcde",
			mcvNamespace:      "spoke1",
			resourceType:      "InstallPlan",
			resourceName:      "installPlan-abcd",
			resourceNamespace: "installPlan-abcd-namespace",
			cguName:           "cgu",
			cguNamespace:      "default",
			validateFunc: func(t *testing.T, runtimeClient client.Client, safeMcvName, mcvName, mcvNamespace,
				resourceType, resourceName, resourceNamespace, cguName, cguNamespace string) {
				mcv, err := EnsureManagedClusterView(context.TODO(), runtimeClient, safeMcvName, mcvName, mcvNamespace,
					resourceType, resourceName, resourceNamespace, cguName, cguNamespace)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, mcv.ObjectMeta.Name, safeMcvName)
				assert.Equal(t, mcv.ObjectMeta.Namespace, mcvNamespace)
				assert.Equal(t, mcv.ObjectMeta.Labels,
					map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": cguName,
						"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": cguNamespace,
					})
				assert.Equal(t, mcv.Spec.Scope.Resource, resourceType)
				assert.Equal(t, mcv.Spec.Scope.Name, resourceName)
				assert.Equal(t, mcv.Spec.Scope.Namespace, resourceNamespace)
			},
		},
		{
			name: "ManagedClusterView is successfully updated",
			mcv: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "view1", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "whatever",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
			},
			mcvName:           "view1",
			safeMcvName:       "view1-abcde",
			mcvNamespace:      "spoke1",
			resourceType:      "InstallPlan",
			resourceName:      "installPlan-abcd",
			resourceNamespace: "installPlan-abcd-namespace",
			cguName:           "cgu",
			cguNamespace:      "default",
			validateFunc: func(t *testing.T, runtimeClient client.Client, safeMcvName, mcvName, mcvNamespace,
				resourceType, resourceName, resourceNamespace, cguName, cguNamespace string) {
				mcv, err := EnsureManagedClusterView(context.TODO(), runtimeClient, safeMcvName, mcvName, mcvNamespace,
					resourceType, resourceName, resourceNamespace, cguName, cguNamespace)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, mcv.ObjectMeta.Name, safeMcvName)
				assert.Equal(t, mcv.ObjectMeta.Namespace, mcvNamespace)
				assert.Equal(t, mcv.ObjectMeta.Labels,
					map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": cguName,
						"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": cguNamespace})
				assert.Equal(t, mcv.Spec.Scope.Resource, resourceType)
				assert.Equal(t, mcv.Spec.Scope.Name, resourceName)
				assert.Equal(t, mcv.Spec.Scope.Namespace, resourceNamespace)
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			if tc.mcv != nil {
				objs = append(objs, tc.mcv)
			}
			fakeClient, err := getFakeClientFromObjects(objs...)

			if err != nil {
				t.Errorf("error in creating fake client")
			}

			tc.validateFunc(t, fakeClient, tc.safeMcvName, tc.mcvName, tc.mcvNamespace,
				tc.resourceType, tc.resourceName, tc.resourceNamespace, tc.cguName, tc.cguNamespace)
		})
	}
}

func TestEnsureInstallPlanIsApproved(t *testing.T) {
	testcases := []struct {
		name              string
		cgu               *ranv1alpha1.ClusterGroupUpgrade
		subscription      operatorsv1alpha1.Subscription
		mcvForInstallPlan *viewv1beta1.ManagedClusterView
		clusterName       string
		validateFunc      func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
			subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView)
	}{
		{
			name: "ManagedClusterView has missing conditions",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installplan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, mcvForInstallPlan.Status.Conditions, []v1.Condition([]v1.Condition(nil)))
				assert.Equal(t, result, MultiCloudPendingStatus)
			},
		},
		{
			name: "ManagedClusterView has condition type different than Processing",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installplan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   "SomeUnexpectedValue",
							Reason: viewv1beta1.ReasonGetResourceFailed,
						},
					},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, MultiCloudPendingStatus)
			},
		},
		{
			name: "ManagedClusterView has condition reason different than GetResourceProcessing",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installplan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResourceFailed,
						},
					},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, MultiCloudPendingStatus)
			},
		},
		{
			name: "ManagedClusterView condition status different from true",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "False",
						},
					},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanCannotBeApproved)
			},
		},
		{
			name: "InstallPlan does not have approval set to Manual",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "InstallPlan",
                          "metadata": {"name": "installPlan-xyz","resourceVersion": "3850433"},
						  "spec": {"approval": "Automatic","approved": true,
						  "clusterServiceVersionNames": ["ptp-operator.4.9.0-202201210133"]}}`,
					)},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanCannotBeApproved)
			},
		},
		{
			name: "InstallPlan is already approved",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "InstallPlan",
                          "metadata": {"name": "installPlan-xyz","resourceVersion": "3850433"},
						  "spec": {"approval": "Manual","approved": true,
						  "clusterServiceVersionNames": ["ptp-operator.4.9.0-202201210133"]}}`,
					)},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanAlreadyApproved)
			},
		},
		{
			name: "MCA was created to approve InstallPlan",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			subscription: operatorsv1alpha1.Subscription{
				Status: operatorsv1alpha1.SubscriptionStatus{
					InstallPlanRef: &corev1.ObjectReference{
						Kind:      "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
					Install: &operatorsv1alpha1.InstallPlanReference{
						Kind: "InstallPlan",
						Name: "installPlan-xyz",
					},
				},
			},
			mcvForInstallPlan: &viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "installPlan-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "InstallPlan",
						Name:      "installPlan-xyz",
						Namespace: "installPlan-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "InstallPlan",
                          "metadata": {"name": "installPlan-xyz","namespace":"installPlan-xyz-namespace",
						  "resourceVersion": "3850433"}, "spec": {"approval": "Manual","approved": false,
						  "clusterServiceVersionNames": ["ptp-operator.4.9.0-202201210133"]}}`,
					)},
				},
			},
			clusterName: "spoke1",
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				subscription operatorsv1alpha1.Subscription, clusterName string, mcvForInstallPlan *viewv1beta1.ManagedClusterView) {
				result, err := EnsureInstallPlanIsApproved(context.TODO(), runtimeClient, cgu, subscription, clusterName)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanWasApproved)

				mcaForInstallPlan := &actionv1beta1.ManagedClusterAction{}
				if err := runtimeClient.Get(context.TODO(), types.NamespacedName{Name: "installPlan-xyz", Namespace: clusterName}, mcaForInstallPlan); err != nil {
					if errors.IsNotFound(err) {
						t.Errorf("Expected Managed Cluster Action installPlan-xyz to have been created in namespace %s, but failed",
							clusterName)
					}
					t.Errorf("Unexpected error: %v", err.Error())
				}

				assert.Equal(t, mcaForInstallPlan.Spec.ActionType, actionv1beta1.UpdateActionType)
				assert.Equal(t, mcaForInstallPlan.Spec.KubeWork.Resource, "installplan")
				assert.Equal(t, mcaForInstallPlan.Spec.KubeWork.Namespace, "installPlan-xyz-namespace")
				assert.Equal(t, mcaForInstallPlan.ObjectMeta.Name, "installPlan-xyz")
				assert.Equal(t, mcaForInstallPlan.ObjectMeta.Namespace, clusterName)
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			objs = append(objs, &tc.subscription)
			if tc.mcvForInstallPlan != nil {
				objs = append(objs, tc.mcvForInstallPlan)
			}
			if tc.cgu != nil {
				objs = append(objs, tc.cgu)
			}

			fakeClient, err := getFakeClientFromObjects(objs...)

			if err != nil {
				t.Errorf("error in creating fake client")
			}

			tc.validateFunc(t, fakeClient, tc.cgu, tc.subscription, tc.clusterName, tc.mcvForInstallPlan)
		})
	}
}

func TestProcessSubscriptionManagedClusterView(t *testing.T) {
	testcases := []struct {
		name               string
		cgu                ranv1alpha1.ClusterGroupUpgrade
		mcvForSubscription viewv1beta1.ManagedClusterView
		clusterName        string
		mockFunc           func() // this will be mocking EnsureInstallPlanIsApproved
		validateFunc       func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
			clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView)
	}{
		{
			name: "ManagedClusterView has missing conditions",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanCannotBeApproved, nil
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, MultiCloudPendingStatus)
			},
		},
		{
			name: "ManagedClusterView has condition reason different than GetResourceProcessing",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonResourceNameInvalid,
							Status: "True",
						},
					},
				},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanCannotBeApproved, nil
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, MultiCloudPendingStatus)
			},
		},
		{
			name: "Subscription status state is different than UpgradePending",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "Subscription",
				          "metadata": {"name": "sub-xyz","namespace":"sub-xyz-namespace",
						  "resourceVersion": "3850622"}, "spec": {"installPlanApproval:": "Manual"},
				     	  "status":{"state":"AtLatestKnown","installPlanRef":{"apiVersion":"operators.coreos.com/v1alpha1",
						  "kind":"InstallPlan","name":"install-jx8q5","namespace":"openshift-ptp",
						  "resourceVersion":"3850433"}}}`,
					)},
				},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanCannotBeApproved, nil
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanCannotBeApproved)
			},
		},
		{
			name: "Subscription status InstallPlanRef is missing",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "Subscription",
				          "metadata": {"name": "sub-xyz","namespace":"sub-xyz-namespace",
						  "resourceVersion": "3850622"}, "spec": {"installPlanApproval:": "Manual"},
				     	  "status":{"state":"UpgradePending"}}`,
					)},
				},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanCannotBeApproved, nil
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err != nil {
					t.Errorf("Error occurred and it wasn't expected")
				}
				assert.Equal(t, result, InstallPlanCannotBeApproved)
			},
		},
		{
			name: "EnsureInstallPlanIsApproved returns error",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "Subscription",
				          "metadata": {"name": "sub-xyz","namespace":"sub-xyz-namespace",
						  "resourceVersion": "3850622"}, "spec": {"installPlanApproval:": "Manual"},
				     	  "status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com/v1alpha1",
						  "kind":"InstallPlan","name":"install-jx8q5","namespace":"openshift-ptp",
						  "resourceVersion":"3850433"},"installplan":{"apiVersion":"operators.coreos.com/v1alpha1",
						  "kind":"InstallPlan","name":"install-jx8q5"}}}`,
					)},
				},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanCannotBeApproved, fmt.Errorf("EnsureInstallPlanIsApproved returned error")
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err == nil {
					t.Errorf("Error was expected, but it didn't happen")
				}
				assert.Equal(t, result, InstallPlanCannotBeApproved)
			},
		},
		{
			name: "EnsureInstallPlanIsApproved returns error",
			cgu: ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
			},
			mcvForSubscription: viewv1beta1.ManagedClusterView{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu-default-subscription-sub-xyz", Namespace: "spoke1",
				},
				Spec: viewv1beta1.ViewSpec{
					Scope: viewv1beta1.ViewScope{
						Resource:  "subscriptions.operators.coreos.com",
						Name:      "sub-xyz",
						Namespace: "sub-xyz-namespace",
					},
				},
				Status: viewv1beta1.ViewStatus{
					Conditions: []v1.Condition{
						{
							Type:   viewv1beta1.ConditionViewProcessing,
							Reason: viewv1beta1.ReasonGetResource,
							Status: "True",
						},
					},
					Result: runtime.RawExtension{Raw: []byte(
						`{"apiVersion": "operators.coreos.com/v1alpha1","kind": "Subscription",
				          "metadata": {"name": "sub-xyz","namespace":"sub-xyz-namespace",
						  "resourceVersion": "3850622"}, "spec": {"installPlanApproval:": "Manual"},
				     	  "status":{"state":"UpgradePending","installPlanRef":{"apiVersion":"operators.coreos.com/v1alpha1",
						  "kind":"InstallPlan","name":"install-jx8q5","namespace":"openshift-ptp",
						  "resourceVersion":"3850433"},"installplan":{"apiVersion":"operators.coreos.com/v1alpha1",
						  "kind":"InstallPlan","name":"install-jx8q5"}}}`,
					)},
				},
			},
			clusterName: "spoke1",
			mockFunc: func() {
				EnsureInstallPlanIsApproved = func(ctx context.Context, c client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
					subscription operatorsv1alpha1.Subscription, clusterName string) (int, error) {
					return InstallPlanWasApproved, nil
				}
			},
			validateFunc: func(t *testing.T, runtimeClient client.Client, cgu *ranv1alpha1.ClusterGroupUpgrade,
				clusterName string, mcvForSubscription *viewv1beta1.ManagedClusterView) {
				result, err := ProcessSubscriptionManagedClusterView(context.TODO(), runtimeClient, cgu, clusterName, mcvForSubscription)
				if err != nil {
					t.Errorf("Error was not expected, but it happened")
				}
				assert.Equal(t, result, InstallPlanWasApproved)
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			objs = append(objs, &tc.mcvForSubscription)
			objs = append(objs, &tc.cgu)

			fakeClient, err := getFakeClientFromObjects(objs...)

			if err != nil {
				t.Errorf("error in creating fake client")
			}

			tc.mockFunc()
			tc.validateFunc(t, fakeClient, &tc.cgu, tc.clusterName, &tc.mcvForSubscription)
		})
	}
}

func TestMultiCloudUtilGetMultiCloudObjectName(t *testing.T) {
	testcase := struct {
		cgu            ranv1alpha1.ClusterGroupUpgrade
		kind           string
		objectName     string
		expectedResult string
	}{

		cgu: ranv1alpha1.ClusterGroupUpgrade{
			ObjectMeta: v1.ObjectMeta{
				Name: "cgu-test", Namespace: "cgu-namespace",
			},
		},
		kind:           "Subscription",
		objectName:     "ptp",
		expectedResult: "cgu-test-cgu-namespace-subscription-ptp",
	}
	result := GetMultiCloudObjectName(&testcase.cgu, testcase.kind, testcase.objectName)
	assert.Equal(t, result, testcase.expectedResult)
}

func TestFinalMultiCloudObjectCleanup(t *testing.T) {
	boolTrue := true
	boolFalse := false
	allClusters := []string{"spoke1", "spoke2", "spoke3"}
	mcvExists := func(t *testing.T, ctx context.Context, client client.Client, cluster string) bool {
		mcv := &viewv1beta1.ManagedClusterView{}
		if err := client.Get(ctx, types.NamespacedName{Name: "view", Namespace: cluster}, mcv); err != nil {
			if errors.IsNotFound(err) {
				return false
			}
			t.Fatal("Error occurred and it wasn't expected", err)
		}
		return true
	}

	mcaExists := func(t *testing.T, ctx context.Context, client client.Client, cluster string) bool {
		mca := &actionv1beta1.ManagedClusterAction{}
		if err := client.Get(ctx, types.NamespacedName{Name: "action", Namespace: cluster}, mca); err != nil {
			if errors.IsNotFound(err) {
				return false
			}
			t.Fatal("Error occurred and it wasn't expected", err)
		}
		return true
	}

	testcases := []struct {
		name              string
		cgu               *ranv1alpha1.ClusterGroupUpgrade
		clustersToCleanup []string
		clustersToIgnore  []string
	}{
		{
			name: "not enabled CGU",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolFalse,
				},
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					RemediationPlan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
				},
			},
			clustersToCleanup: []string{},
			clustersToIgnore:  allClusters,
		},
		{
			name: "completed CGU",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolTrue,
				},
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					RemediationPlan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
					Status: ranv1alpha1.UpgradeStatus{
						CompletedAt: metav1.Now(),
					},
				},
			},
			clustersToCleanup: []string{},
			clustersToIgnore:  allClusters,
		},
		{
			name: "enabled but blocked CGU with no remediation plan",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolTrue,
				},
			},
			clustersToCleanup: []string{},
			clustersToIgnore:  []string{"spoke1", "spoke2", "spoke3"},
		},
		{
			name: "in progress CGU - batch 1",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolTrue,
				},
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					Clusters: []ranv1alpha1.ClusterState{
						{Name: "spoke1", State: ClusterRemediationComplete},
					},
					RemediationPlan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatch: 1,
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"spoke1": {State: ranv1alpha1.Completed},
							"spoke2": {State: ranv1alpha1.InProgress},
						},
					},
				},
			},
			clustersToCleanup: []string{"spoke2"},
			clustersToIgnore:  []string{"spoke1", "spoke3"},
		},
		{
			name: "in progress CGU - batch 2",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolTrue,
				},
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					Clusters: []ranv1alpha1.ClusterState{
						{Name: "spoke1", State: ClusterRemediationComplete},
						{Name: "spoke2", State: ClusterRemediationTimedout},
					},
					RemediationPlan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
					Status: ranv1alpha1.UpgradeStatus{
						CurrentBatch: 2,
						CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
							"spoke3": {State: ranv1alpha1.InProgress},
						},
					},
				},
			},
			clustersToCleanup: []string{"spoke3"},
			clustersToIgnore:  []string{"spoke1", "spoke2"},
		},
		{
			// This is an edge case where CGU is deleted while it's starting the first batch
			name: "CGU with no current batch",
			cgu: &ranv1alpha1.ClusterGroupUpgrade{
				ObjectMeta: v1.ObjectMeta{
					Name: "cgu", Namespace: "default",
				},
				Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
					Enable: &boolTrue,
				},
				Status: ranv1alpha1.ClusterGroupUpgradeStatus{
					RemediationPlan: [][]string{{"spoke1", "spoke2"}, {"spoke3"}},
				},
			},
			clustersToCleanup: []string{"spoke1", "spoke2"},
			clustersToIgnore:  []string{"spoke3"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			for _, cluster := range allClusters {
				objs = append(objs, &viewv1beta1.ManagedClusterView{

					ObjectMeta: v1.ObjectMeta{
						Name:      "view",
						Namespace: cluster,
						Labels: map[string]string{
							"openshift-cluster-group-upgrades/clusterGroupUpgrade":          tc.cgu.Name,
							"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": tc.cgu.Namespace,
						},
					},
				})
				objs = append(objs, &actionv1beta1.ManagedClusterAction{

					ObjectMeta: v1.ObjectMeta{
						Name:      "action",
						Namespace: cluster,
						Labels: map[string]string{
							"openshift-cluster-group-upgrades/clusterGroupUpgrade": tc.cgu.Namespace + "-" + tc.cgu.Name,
						},
					},
				})
			}
			if tc.cgu != nil {
				objs = append(objs, tc.cgu)
			}

			client, err := getFakeClientFromObjects(objs...)

			if err != nil {
				t.Errorf("error in creating fake client: %v", err)
			}
			ctx := context.TODO()
			err = FinalMultiCloudObjectCleanup(ctx, client, tc.cgu)
			if err != nil {
				t.Fatal("Error occurred and it wasn't expected:", err)
			}

			for _, cluster := range tc.clustersToIgnore {
				assert.True(t, mcvExists(t, ctx, client, cluster))
				assert.True(t, mcaExists(t, ctx, client, cluster))
			}

			for _, cluster := range tc.clustersToCleanup {
				assert.False(t, mcvExists(t, ctx, client, cluster))
				assert.False(t, mcaExists(t, ctx, client, cluster))
			}
		})
	}
}
