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

package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// selfSignedCA returns a PEM-encoded self-signed CA with a recognisable subject, so a
// test can check that a pool built from it contains this certificate.
func selfSignedCA(t *testing.T, commonName string) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	assert.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	assert.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert
}

// TestSeedRootPool verifies that the cluster's image-pull CAs are added to the pool used
// for the seed-image registry handshake, and that a hub with no such CAs (or an
// unparsable bundle) keeps the default verification instead of failing closed.
func TestSeedRootPool(t *testing.T) {
	caBundle, caCert := selfSignedCA(t, "kni-qe-lab-registry-ca")

	t.Run("no cluster CAs leaves the default verification", func(t *testing.T) {
		assert.Nil(t, seedRootPool(nil))
		assert.Nil(t, seedRootPool([]byte{}))
	})

	t.Run("an unparsable bundle leaves the default verification", func(t *testing.T) {
		assert.Nil(t, seedRootPool([]byte("not a certificate")))
	})

	t.Run("the cluster CA is added to the system pool", func(t *testing.T) {
		pool := seedRootPool(caBundle)
		assert.NotNil(t, pool)
		assert.Contains(t, pool.Subjects(), caCert.RawSubject,
			"the cluster CA must be in the pool used for the registry handshake")
	})

	t.Run("a bundle with several CAs adds all of them", func(t *testing.T) {
		otherBundle, otherCert := selfSignedCA(t, "second-registry-ca")
		pool := seedRootPool(append(append([]byte{}, caBundle...), otherBundle...))
		assert.NotNil(t, pool)
		assert.Contains(t, pool.Subjects(), caCert.RawSubject)
		assert.Contains(t, pool.Subjects(), otherCert.RawSubject)
	})

	t.Run("the transport carries the pool", func(t *testing.T) {
		transport := seedRegistryTransport(caBundle)
		assert.NotNil(t, transport)
	})
}
