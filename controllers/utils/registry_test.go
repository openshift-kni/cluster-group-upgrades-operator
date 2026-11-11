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
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	"github.com/stretchr/testify/assert"
)

func dockerConfigFor(t *testing.T, registry, user, pass string) []byte {
	t.Helper()
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return []byte(`{"auths":{"` + registry + `":{"auth":"` + auth + `"}}}`)
}

func TestKeychainFromDockerConfigJSON(t *testing.T) {
	t.Run("empty config resolves anonymously", func(t *testing.T) {
		kc, err := KeychainFromDockerConfigJSON(nil)
		assert.NoError(t, err)
		ref, err := name.ParseReference("quay.io/example/seed:latest")
		assert.NoError(t, err)
		authenticator, err := kc.Resolve(ref.Context())
		assert.NoError(t, err)
		assert.Equal(t, authn.Anonymous, authenticator)
	})

	t.Run("matching registry resolves to its credentials", func(t *testing.T) {
		kc, err := KeychainFromDockerConfigJSON(dockerConfigFor(t, "quay.io", "alice", "s3cret"))
		assert.NoError(t, err)
		ref, err := name.ParseReference("quay.io/example/seed:latest")
		assert.NoError(t, err)
		authenticator, err := kc.Resolve(ref.Context())
		assert.NoError(t, err)
		cfg, err := authenticator.Authorization()
		assert.NoError(t, err)
		assert.Equal(t, "alice", cfg.Username)
		assert.Equal(t, "s3cret", cfg.Password)
	})

	t.Run("auth key with scheme and path still matches by host", func(t *testing.T) {
		kc, err := KeychainFromDockerConfigJSON(dockerConfigFor(t, "https://registry.example.com/v2/", "bob", "pw"))
		assert.NoError(t, err)
		ref, err := name.ParseReference("registry.example.com/mirror/seed:v1")
		assert.NoError(t, err)
		authenticator, err := kc.Resolve(ref.Context())
		assert.NoError(t, err)
		cfg, err := authenticator.Authorization()
		assert.NoError(t, err)
		assert.Equal(t, "bob", cfg.Username)
	})

	t.Run("non-matching registry resolves anonymously", func(t *testing.T) {
		kc, err := KeychainFromDockerConfigJSON(dockerConfigFor(t, "quay.io", "alice", "s3cret"))
		assert.NoError(t, err)
		ref, err := name.ParseReference("registry.redhat.io/example/seed:latest")
		assert.NoError(t, err)
		authenticator, err := kc.Resolve(ref.Context())
		assert.NoError(t, err)
		assert.Equal(t, authn.Anonymous, authenticator)
	})

	// Regression: a bare "docker.io" auth key must match a Docker Hub seed
	// reference, whose RegistryStr() go-containerregistry canonicalises to
	// "index.docker.io".
	t.Run("bare docker.io auth key matches a Docker Hub reference", func(t *testing.T) {
		kc, err := KeychainFromDockerConfigJSON(dockerConfigFor(t, "docker.io", "carol", "pw"))
		assert.NoError(t, err)
		ref, err := name.ParseReference("docker.io/library/seed:latest")
		assert.NoError(t, err)
		authenticator, err := kc.Resolve(ref.Context())
		assert.NoError(t, err)
		cfg, err := authenticator.Authorization()
		assert.NoError(t, err)
		assert.Equal(t, "carol", cfg.Username)
		assert.Equal(t, "pw", cfg.Password)
	})

	t.Run("invalid json is an error", func(t *testing.T) {
		_, err := KeychainFromDockerConfigJSON([]byte("not json"))
		assert.Error(t, err)
	})
}

func TestRegistryHostFromAuthKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{key: "quay.io", want: "quay.io"},
		{key: "https://registry.example.com/v2/", want: "registry.example.com"},
		{key: "docker.io", want: "index.docker.io"},
		{key: "https://docker.io/v1/", want: "index.docker.io"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			assert.Equal(t, test.want, registryHostFromAuthKey(test.key))
		})
	}
}

func TestCheckSeedConfigSize(t *testing.T) {
	t.Run("in-range size is accepted", func(t *testing.T) {
		assert.NoError(t, checkSeedConfigSize("quay.io/example/seed:latest", 4096))
	})
	t.Run("zero size is accepted", func(t *testing.T) {
		assert.NoError(t, checkSeedConfigSize("quay.io/example/seed:latest", 0))
	})
	t.Run("negative size is rejected", func(t *testing.T) {
		assert.Error(t, checkSeedConfigSize("quay.io/example/seed:latest", -1))
	})
	t.Run("oversized config is rejected", func(t *testing.T) {
		assert.Error(t, checkSeedConfigSize("quay.io/example/seed:latest", maxSeedConfigSize+1))
	})
}

// recordingRoundTripper is a base transport stub that records the last request it
// saw and returns a canned response without any network egress.
type recordingRoundTripper struct {
	called bool
	req    *http.Request
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.called = true
	rt.req = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestHTTPSOnlyRoundTripper(t *testing.T) {
	t.Run("non-HTTPS request is refused before reaching the base transport", func(t *testing.T) {
		base := &recordingRoundTripper{}
		transport := httpsOnlyRoundTripper{base: base}
		req, err := http.NewRequest(http.MethodGet, "http://registry.example.com/v2/", nil)
		assert.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.False(t, base.called)
	})

	t.Run("HTTPS request is delegated to the base transport", func(t *testing.T) {
		base := &recordingRoundTripper{}
		transport := httpsOnlyRoundTripper{base: base}
		req, err := http.NewRequest(http.MethodGet, "https://registry.example.com/v2/", nil)
		assert.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, base.called)
	})
}

func configWithLabel(labels map[string]string) *v1.ConfigFile {
	return &v1.ConfigFile{Config: v1.Config{Labels: labels}}
}

func TestSeedFormatFromConfig(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *v1.ConfigFile
		want      int
		wantError bool
	}{
		{
			name: "valid integer label",
			cfg:  configWithLabel(map[string]string{lcav1.SeedFormatOCILabel: "5"}),
			want: 5,
		},
		{
			name: "whitespace is trimmed",
			cfg:  configWithLabel(map[string]string{lcav1.SeedFormatOCILabel: " 4 "}),
			want: 4,
		},
		{
			name:      "missing label is an error",
			cfg:       configWithLabel(map[string]string{"other": "1"}),
			wantError: true,
		},
		{
			name:      "non-integer label is an error",
			cfg:       configWithLabel(map[string]string{lcav1.SeedFormatOCILabel: "four"}),
			wantError: true,
		},
		{
			name:      "nil config is an error",
			cfg:       nil,
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := seedFormatFromConfig("quay.io/example/seed:latest", test.cfg)
			if test.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}
