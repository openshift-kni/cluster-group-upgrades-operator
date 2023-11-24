package client

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	"k8s.io/klog"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
)

var (
	// Client defines the API client to run CRUD operations, that will be used for testing.
	Client client.Client
	// K8sClient defines k8s client to run subresource operations, for example you should use it to get pod logs.
	K8sClient *kubernetes.Clientset
	// ClientsEnabled tells if the client from the package can be used.
	ClientsEnabled bool
)

func init() {
	// Setup Scheme for all resources.
	if err := ranv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		klog.Exit(err.Error())
	}
	if err := viewv1beta1.AddToScheme(scheme.Scheme); err != nil {
		klog.Exit(err.Error())
	}

	var err error
	Client, err = New()
	if err != nil {
		ClientsEnabled = false
		return
	}
	K8sClient, err = NewK8s()
	if err != nil {
		ClientsEnabled = false
		return
	}
	ClientsEnabled = true
}

// New returns a controller-runtime client.
func New() (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	c, err := client.New(cfg, client.Options{})
	return c, err
}

// NewK8s Returns a kubernetes clientset.
func NewK8s() (*kubernetes.Clientset, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Exit(err.Error())
	}
	return clientset, nil
}
