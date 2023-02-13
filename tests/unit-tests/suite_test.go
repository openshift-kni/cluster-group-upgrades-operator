package controllers_test

import (
	"context"
	"flag"
	//"log"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	testclient "github.com/openshift-kni/cluster-group-upgrades-operator/tests/pkg/client"
	testutils "github.com/openshift-kni/cluster-group-upgrades-operator/tests/pkg/utils"
)

var junitPath *string
var reportPath *string

func init() {
	junitPath = flag.String("junit", "", "the path for the junit format report")
	reportPath = flag.String("report", "", "the path of the report file containing details for failed tests")
}

func TestTest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Upgrades Operator unit tests")
}

var _ = BeforeSuite(func() {
	By("Setup k8s client")
	Expect(testclient.ClientsEnabled).To(BeTrue(), "package client not enabled")
	// Create test namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testutils.TestingNamespace,
		},
	}
	err := testclient.Client.Create(context.TODO(), namespace)

	if errors.IsAlreadyExists(err) {
		return
	}
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {

})
