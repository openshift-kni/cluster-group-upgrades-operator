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
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
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

	t.Run("invalid json is an error", func(t *testing.T) {
		_, err := KeychainFromDockerConfigJSON([]byte("not json"))
		assert.Error(t, err)
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
			cfg:  configWithLabel(map[string]string{SeedFormatOCILabel: "5"}),
			want: 5,
		},
		{
			name: "whitespace is trimmed",
			cfg:  configWithLabel(map[string]string{SeedFormatOCILabel: " 4 "}),
			want: 4,
		},
		{
			name:      "missing label is an error",
			cfg:       configWithLabel(map[string]string{"other": "1"}),
			wantError: true,
		},
		{
			name:      "non-integer label is an error",
			cfg:       configWithLabel(map[string]string{SeedFormatOCILabel: "four"}),
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
