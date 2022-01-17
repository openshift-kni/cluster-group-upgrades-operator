module github.com/openshift-kni/cluster-group-upgrades-operator

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/open-cluster-management/governance-policy-propagator v0.0.0-20211209191640-b399eb8a0bf2
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616 // indirect
	golang.org/x/tools v0.1.8 // indirect
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	kubevirt.io/qe-tools v0.1.7
	open-cluster-management.io/api v0.5.0
	sigs.k8s.io/controller-runtime v0.9.2
)

replace k8s.io/client-go => k8s.io/client-go v0.21.3
