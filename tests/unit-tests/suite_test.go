package controllers_test

import (
	"context"
	"flag"
	//"log"
	"path"
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ginkgo_reporters "kubevirt.io/qe-tools/pkg/ginkgo-reporters"

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

	rr := []Reporter{}
	if ginkgo_reporters.Polarion.Run {
		rr = append(rr, &ginkgo_reporters.Polarion)
	}

	if *junitPath != "" {
		junitFile := path.Join(*junitPath, "validation_junit.xml")
		rr = append(rr, reporters.NewJUnitReporter(junitFile))
	}

	RunSpecsWithDefaultAndCustomReporters(t, "Upgrades Operator unit tests", rr)
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
