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

package controllers_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	//. "github.com/openshift-kni/cluster-group-upgrades-operator/controllers"
	testclient "github.com/openshift-kni/cluster-group-upgrades-operator/tests/pkg/client"
	testutils "github.com/openshift-kni/cluster-group-upgrades-operator/tests/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	ctx           = context.Background()
	fetched       = &ranv1alpha1.Group{}
	groupTestname = "group1"
)

var _ = Describe("Group Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("When adding a Group object", func() {
		groupInstance := &ranv1alpha1.Group{
			ObjectMeta: metav1.ObjectMeta{
				Name:      groupTestname,
				Namespace: testutils.TestingNamespace,
			},
		}
		It("The groups.ran.openshift.io CR should be created successfully", func() {
			Expect(testclient.Client.Create(ctx, groupInstance)).Should(Succeed())

		})
		It("The created CR should exist in the expected namespace: "+testutils.TestingNamespace, func() {
			Eventually(func() error {
				err := testclient.Client.Get(ctx, types.NamespacedName{Name: groupInstance.Name, Namespace: testutils.TestingNamespace}, fetched)
				return err
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
		It("Should also be deleted successfully", func() {
			Expect(testclient.Client.Delete(ctx, groupInstance)).Should(Succeed())

		})
	})

	/*
		Context("When calling ensureBatchPlacementRules", func() {
			groupInstance := &ranv1alpha1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "group1",
					Namespace: testutils.TestingNamespace,
				},
			}

			var remediationPlan [][]string
			remediationPlan[0][0] = "site1"
			remediationPlan[0][1] = "site2"

			It("return an err", func() {
				var gr *GroupReconciler
				gr = (&GroupReconciler{
					Client: testclient.Client,
					Log:    ctrl.Log.WithName("controllers").WithName("Group"),
				})

				//gr.EnsureBatchPlacementRules(ctx, groupInstance, remediationPlan[0], 1)
				//gr.NewSitePlacementRule(ctx, groupInstance, 1, "site1")
			})
		})
	*/
})
