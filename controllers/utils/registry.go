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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// seedImageReadTimeout bounds the registry read performed by the default
// SeedImageReader. remote.DefaultTransport limits the dial and TLS handshake
// but not ResponseHeaderTimeout, and controller-runtime sets no deadline on the
// Reconcile context, so without this bound a registry that accepts the
// connection and then stalls would hold the reconcile worker indefinitely. A
// timeout instead surfaces as the expected SeedImageUnreadable block and the
// IBGU requeues.
const seedImageReadTimeout = 30 * time.Second

// SeedImageReader reads the seed format version an image publishes through its
// OCI config label (SeedFormatOCILabel). It is an interface so the IBGU
// controller can inject a fake in tests and avoid real registry egress.
type SeedImageReader interface {
	// ReadSeedFormatVersion resolves imageRef against its registry using the
	// supplied .dockerconfigjson bytes (nil/empty means anonymous access) and
	// returns the integer value of the SeedFormatOCILabel on the image config.
	// It returns an error when the reference is invalid, the image cannot be
	// fetched, the label is absent, or its value is not an integer.
	ReadSeedFormatVersion(ctx context.Context, imageRef string, dockerConfigJSON []byte) (int, error)
}

// remoteSeedImageReader is the default SeedImageReader; it reads the image
// config directly from the registry with go-containerregistry. Reaching the
// (mirror) registry and trusting its CA is a hub prerequisite for the seed
// compatibility pre-flight (CNF-29242).
type remoteSeedImageReader struct{}

// NewSeedImageReader returns the default registry-backed SeedImageReader.
func NewSeedImageReader() SeedImageReader {
	return &remoteSeedImageReader{}
}

func (remoteSeedImageReader) ReadSeedFormatVersion(ctx context.Context, imageRef string, dockerConfigJSON []byte) (int, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return 0, fmt.Errorf("invalid seed image reference %q: %w", imageRef, err)
	}
	keychain, err := KeychainFromDockerConfigJSON(dockerConfigJSON)
	if err != nil {
		return 0, err
	}
	// Bound the registry read so a stalled registry cannot hold the reconcile
	// worker. readCtx must stay active until img.ConfigFile() returns, because
	// the config blob fetch also uses this context.
	readCtx, cancel := context.WithTimeout(ctx, seedImageReadTimeout)
	defer cancel()
	img, err := remote.Image(ref, remote.WithContext(readCtx), remote.WithAuthFromKeychain(keychain))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch seed image %q: %w", imageRef, err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return 0, fmt.Errorf("failed to read seed image %q config: %w", imageRef, err)
	}
	return seedFormatFromConfig(imageRef, cfg)
}

// seedFormatFromConfig extracts and parses the SeedFormatOCILabel value from an
// image config. It is split out so the label-parsing branches can be unit tested
// without a live registry.
func seedFormatFromConfig(imageRef string, cfg *v1.ConfigFile) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("seed image %q has no config", imageRef)
	}
	raw, ok := cfg.Config.Labels[SeedFormatOCILabel]
	if !ok {
		return 0, fmt.Errorf("seed image %q has no %s label", imageRef, SeedFormatOCILabel)
	}
	format, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("seed image %q has non-integer %s label %q: %w", imageRef, SeedFormatOCILabel, raw, err)
	}
	return format, nil
}

// dockerConfigJSON is the subset of a Kubernetes .dockerconfigjson secret value
// the keychain needs.
type dockerConfigJSON struct {
	Auths map[string]authn.AuthConfig `json:"auths"`
}

// dockerConfigKeychain resolves credentials from a parsed .dockerconfigjson by
// matching the target registry host against the configured auth entries.
type dockerConfigKeychain struct {
	auths map[string]authn.AuthConfig
}

// KeychainFromDockerConfigJSON builds an authn.Keychain from the bytes of a
// Kubernetes .dockerconfigjson secret value. Empty input yields a keychain that
// always resolves anonymously, which is the correct behaviour for a public seed
// image referenced without a pull secret.
func KeychainFromDockerConfigJSON(raw []byte) (authn.Keychain, error) {
	if len(raw) == 0 {
		return &dockerConfigKeychain{}, nil
	}
	parsed := dockerConfigJSON{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse .dockerconfigjson: %w", err)
	}
	return &dockerConfigKeychain{auths: parsed.Auths}, nil
}

// Resolve implements authn.Keychain. It matches target's registry host against
// the auth entries (tolerating the scheme prefixes and trailing paths Docker
// writes into config keys) and falls back to anonymous access when no entry
// matches.
func (k *dockerConfigKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	host := target.RegistryStr()
	for key, cfg := range k.auths {
		if registryHostFromAuthKey(key) == host {
			return authn.FromConfig(cfg), nil
		}
	}
	return authn.Anonymous, nil
}

// registryHostFromAuthKey normalises a Docker config auth key (which may be a
// bare host, "https://host", "host/v2/", etc.) down to its registry host so it
// can be compared against a Resource's RegistryStr().
func registryHostFromAuthKey(key string) string {
	if i := strings.Index(key, "://"); i >= 0 {
		key = key[i+len("://"):]
	}
	if i := strings.Index(key, "/"); i >= 0 {
		key = key[:i]
	}
	return key
}
