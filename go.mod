module github.com/openshift-kni/cluster-group-upgrades-operator

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.16.0
	github.com/open-cluster-management/governance-policy-propagator v0.0.0-20211209191640-b399eb8a0bf2
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	kubevirt.io/qe-tools v0.1.7
	open-cluster-management.io/api v0.5.0
	sigs.k8s.io/controller-runtime v0.9.2
)

replace k8s.io/client-go => k8s.io/client-go v0.21.3
