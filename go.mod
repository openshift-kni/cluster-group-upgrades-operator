module github.com/openshift-kni/cluster-group-upgrades-operator

go 1.15

require (
	github.com/go-logr/logr v0.3.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/klog v1.0.0
	kubevirt.io/qe-tools v0.1.7
	sigs.k8s.io/controller-runtime v0.8.3
)
