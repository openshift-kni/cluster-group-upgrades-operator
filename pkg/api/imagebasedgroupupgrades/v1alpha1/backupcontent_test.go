package v1alpha1

import (
	"os"
	"testing"

	lcabcv1 "github.com/openshift-kni/lifecycle-agent/api/backupcontent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gopkg.in/yaml.v3"
)

func int32Ptr(i int32) *int32 { return &i }

// TestBackupContentDeepCopy verifies the generated deepcopy produces an
// independent, equal copy (the spec is LCA's BackupContentSpec directly).
func TestBackupContentDeepCopy(t *testing.T) {
	original := &BackupContent{
		ObjectMeta: metav1.ObjectMeta{Name: "ibu-platform-and-apps", Namespace: "default"},
		Spec: lcabcv1.BackupContentSpec{
			BackupWave:  int32Ptr(2),
			RestoreWave: int32Ptr(2),
			Namespaces:  []string{"open-cluster-management-agent"},
			NamespacedResources: []lcabcv1.ResourceSelector{
				{Kind: "deployments.apps", Names: []string{"klusterlet"}},
			},
		},
	}

	// DeepCopyObject must return a non-nil, equal runtime.Object.
	obj := original.DeepCopyObject()
	require.NotNil(t, obj)
	copied, ok := obj.(*BackupContent)
	require.True(t, ok, "DeepCopyObject must return a *BackupContent")
	assert.Equal(t, original, copied)

	// Mutating the copy must not affect the original (true deep copy of the
	// spec's pointer/slice fields).
	*copied.Spec.BackupWave = 99
	copied.Spec.Namespaces[0] = "mutated"
	copied.Spec.NamespacedResources[0].Names[0] = "mutated"
	assert.Equal(t, int32(2), *original.Spec.BackupWave)
	assert.Equal(t, "open-cluster-management-agent", original.Spec.Namespaces[0])
	assert.Equal(t, "klusterlet", original.Spec.NamespacedResources[0].Names[0])
}

// TestBackupContentListDeepCopy verifies the list type's deepcopy.
func TestBackupContentListDeepCopy(t *testing.T) {
	original := &BackupContentList{
		Items: []BackupContent{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}},
		},
	}
	obj := original.DeepCopyObject()
	require.NotNil(t, obj)
	copied, ok := obj.(*BackupContentList)
	require.True(t, ok)
	assert.Equal(t, original, copied)
}

// crdDoc is a minimal view of the generated CRD, enough to assert the
// immutability marker and scope survive regeneration.
type crdDoc struct {
	Spec struct {
		Scope    string `yaml:"scope"`
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties struct {
						Spec struct {
							XValidations []struct {
								Rule string `yaml:"rule"`
							} `yaml:"x-kubernetes-validations"`
						} `yaml:"spec"`
					} `yaml:"properties"`
				} `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

// TestBackupContentCRDImmutabilityRule guards against accidental loss of the
// CEL immutability marker during CRD regeneration: the generated CRD schema
// must be namespaced and carry a "self == oldSelf" rule on spec.
func TestBackupContentCRDImmutabilityRule(t *testing.T) {
	data, err := os.ReadFile("../../../../config/crd/bases/lcm.openshift.io_backupcontents.yaml")
	require.NoError(t, err, "generated BackupContent CRD must exist")

	var crd crdDoc
	require.NoError(t, yaml.Unmarshal(data, &crd))

	assert.Equal(t, "Namespaced", crd.Spec.Scope, "BackupContent must be namespaced")
	require.NotEmpty(t, crd.Spec.Versions)

	var found bool
	for _, v := range crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.XValidations {
		if v.Rule == "self == oldSelf" {
			found = true
			break
		}
	}
	assert.True(t, found, "spec must carry the 'self == oldSelf' immutability rule")
}
