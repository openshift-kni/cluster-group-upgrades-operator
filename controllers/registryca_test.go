/*
Copyright 2026.

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
	"testing"

	"github.com/go-logr/logr"
	ocpv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
)

func configMaps(name, namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       data,
	}
}

// TestRegistryTrustBundle covers how the seed-image pre-flight collects the CAs the
// cluster trusts for image pulls: the ConfigMap referenced by
// image.config.openshift.io/cluster.spec.additionalTrustedCA, and the merged cluster
// bundle. A hub with neither simply yields nothing, so the registry read falls back to
// the operator image's system CAs.
func TestRegistryTrustBundle(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, corev1.AddToScheme(scheme))
	assert.NoError(t, ocpv1.AddToScheme(scheme))

	imageConfig := func(name string) *ocpv1.Image {
		image := &ocpv1.Image{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
		image.Spec.AdditionalTrustedCA.Name = name
		return image
	}

	reconcilerFor := func(objects ...client.Object) *IBGUReconciler {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		return &IBGUReconciler{Client: fakeClient, APIReader: fakeClient, Log: logr.Discard()}
	}

	t.Run("no image config and no bundle yields no CAs", func(t *testing.T) {
		bundle, err := reconcilerFor().registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Empty(t, bundle)
	})

	t.Run("image additionalTrustedCA contribues every registry key", func(t *testing.T) {
		r := reconcilerFor(
			imageConfig("disconnected-edge"),
			configMaps("disconnected-edge", "openshift-config", map[string]string{
				"registry.kni-qe-119.telcoqe.eng.rdu2.dc.redhat.com..5000": "CA-ONE",
				"image-registry.openshift-image-registry.svc..5000":        "CA-TWO",
			}),
		)
		bundle, err := r.registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Contains(t, string(bundle), "CA-ONE")
		assert.Contains(t, string(bundle), "CA-TWO")
	})

	t.Run("the merged cluster bundle is included", func(t *testing.T) {
		r := reconcilerFor(configMaps("trusted-ca-bundle", "openshift-config-managed",
			map[string]string{"ca-bundle.crt": "MERGED-BUNDLE"}))
		bundle, err := r.registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Contains(t, string(bundle), "MERGED-BUNDLE")
	})

	t.Run("both sources are merged", func(t *testing.T) {
		r := reconcilerFor(
			imageConfig("disconnected-edge"),
			configMaps("disconnected-edge", "openshift-config",
				map[string]string{"registry.example:5000": "REGISTRY-CA"}),
			configMaps("trusted-ca-bundle", "openshift-config-managed",
				map[string]string{"ca-bundle.crt": "MERGED-BUNDLE"}),
		)
		bundle, err := r.registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Contains(t, string(bundle), "REGISTRY-CA")
		assert.Contains(t, string(bundle), "MERGED-BUNDLE")
	})

	t.Run("a referenced ConfigMap that is absent is not an error", func(t *testing.T) {
		r := reconcilerFor(imageConfig("disconnected-edge"))
		bundle, err := r.registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Empty(t, bundle)
	})

	t.Run("an empty reference adds nothing", func(t *testing.T) {
		r := reconcilerFor(imageConfig(""))
		bundle, err := r.registryTrustBundle(context.TODO())
		assert.NoError(t, err)
		assert.Empty(t, bundle)
	})
}

// TestSeedImageReaderReceivesClusterCAs proves the resolved bundle reaches the reader:
// the pre-flight must pass it, or the private registry is unverifiable again.
func TestSeedImageReaderReceivesClusterCAs(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, corev1.AddToScheme(scheme))
	assert.NoError(t, ocpv1.AddToScheme(scheme))

	image := &ocpv1.Image{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	image.Spec.AdditionalTrustedCA.Name = "disconnected-edge"
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		image,
		configMaps("disconnected-edge", "openshift-config",
			map[string]string{"registry.example:5000": "REGISTRY-CA"}),
	).Build()

	reader := &capturingSeedImageReader{}
	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{Name: "ibgu", Namespace: "default"},
	}
	ibgu.Spec.IBUSpec.SeedImageRef.Image = "registry.example:5000/seed:1"

	reconciler := &IBGUReconciler{
		Client: fakeClient, APIReader: fakeClient, Log: logr.Discard(), SeedImageReader: reader,
	}
	_, reason, err := reconciler.readSeedImageFormat(context.TODO(), ibgu)
	assert.NoError(t, err)
	assert.Empty(t, reason)
	assert.Contains(t, string(reader.clusterCAs), "REGISTRY-CA",
		"the reader must verify the registry against the cluster's image-pull CAs")
}

// capturingSeedImageReader records the CA bundle it is handed.
type capturingSeedImageReader struct {
	clusterCAs []byte
}

func (c *capturingSeedImageReader) ReadSeedFormatVersion(
	_ context.Context, _ string, _ []byte, clusterCAs []byte,
) (int, error) {
	c.clusterCAs = clusterCAs
	return 5, nil
}

var _ utils.SeedImageReader = &capturingSeedImageReader{}
