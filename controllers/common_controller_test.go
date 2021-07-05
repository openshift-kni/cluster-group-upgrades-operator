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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ranv1alpha1 "github.com/redhat-ztp/cluster-group-lcm/api/v1alpha1"
)

var _ = Describe("Common Controller", func() {

	const timeout = time.Second * 30
	const interval = time.Second * 1

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("When creating Common object", func() {
		It("Should create successfully if name is set to common", func() {
			common := &ranv1alpha1.Common{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ran.openshift.io/v1alpha1",
					Kind:       "Common",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "commony",
					Namespace: "default",
				},
				Spec: ranv1alpha1.CommonSpec{},
			}

			Expect(k8sClient.Create(context.Background(), common)).Should(Succeed())
		})
	})
})
