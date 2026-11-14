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
	"net/http"
	"strconv"
	"strings"

	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// seedImageReadTimeout bounds the registry read performed by the default
// SeedImageReader. remote.DefaultTransport limits the dial and TLS handshake
// but not ResponseHeaderTimeout, and controller-runtime sets no deadline on the
// Reconcile context, so without this bound a registry that accepts the
// connection and then stalls would hold the reconcile worker indefinitely. A
// timeout instead surfaces as a terminal SeedImageUnreadable block rather than
// hanging the reconcile worker.
const seedImageReadTimeout = 30 * time.Second

// maxSeedConfigSize caps the declared image-config blob size the default reader
// will fetch. An image config, even with the seed labels, is a few KiB; 4 MiB is
// ample headroom. go-containerregistry verifies the config digest only after the
// whole blob is downloaded, so a corrupt or hostile manifest advertising a huge
// config would otherwise be fetched in full before the digest check could reject
// it. Checking the manifest-declared size up front turns that into the expected
// SeedImageUnreadable block.
const maxSeedConfigSize = 4 << 20 // 4 MiB

// dockerHubAuthKeyAlias is the bare Docker Hub host that appears in
// .dockerconfigjson auth keys. go-containerregistry canonicalises it to
// name.DefaultRegistry ("index.docker.io") on the reference side, so the keychain
// must normalise the same alias to match.
const dockerHubAuthKeyAlias = "docker.io"

// SeedImageReader reads the seed format version an image publishes through its
// OCI config label (lcav1.SeedFormatOCILabel). It is an interface so the IBGU
// controller can inject a fake in tests and avoid real registry egress.
type SeedImageReader interface {
	// ReadSeedFormatVersion resolves imageRef against its registry using the
	// supplied .dockerconfigjson bytes (nil/empty means anonymous access) and
	// returns the integer value of the lcav1.SeedFormatOCILabel on the image config.
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
	// Refuse any non-HTTPS request so the pull-secret credentials are never sent
	// in cleartext; a registry that would need plain HTTP surfaces as the expected
	// SeedImageUnreadable block.
	transport := httpsOnlyRoundTripper{base: remote.DefaultTransport}
	img, err := remote.Image(ref,
		remote.WithContext(readCtx),
		remote.WithAuthFromKeychain(keychain),
		remote.WithTransport(transport),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch seed image %q: %w", imageRef, err)
	}
	// Reject an oversized (or negative) declared config size before fetching the
	// blob: ConfigFile() downloads the whole config before go-containerregistry
	// verifies its digest, so the manifest-declared size is the only cap available
	// up front.
	manifest, err := img.Manifest()
	if err != nil {
		return 0, fmt.Errorf("failed to read seed image %q manifest: %w", imageRef, err)
	}
	if err := checkSeedConfigSize(imageRef, manifest.Config.Size); err != nil {
		return 0, err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return 0, fmt.Errorf("failed to read seed image %q config: %w", imageRef, err)
	}
	return seedFormatFromConfig(imageRef, cfg)
}

// checkSeedConfigSize rejects a manifest-declared image-config size that is
// negative or above maxSeedConfigSize. It is split out so both rejections can be
// unit tested without a live registry.
func checkSeedConfigSize(imageRef string, size int64) error {
	if size < 0 {
		return fmt.Errorf("seed image %q declares a negative config size (%d)", imageRef, size)
	}
	if size > maxSeedConfigSize {
		return fmt.Errorf("seed image %q declares a config size (%d bytes) above the %d-byte limit",
			imageRef, size, maxSeedConfigSize)
	}
	return nil
}

// httpsOnlyRoundTripper wraps a base transport and refuses any request whose URL
// scheme is not HTTPS, so pull-secret credentials cannot be sent over plain HTTP.
// Supporting genuinely insecure registries is out of scope for the seed-format
// pre-flight (CNF-29242); labs can use the opt-out annotation meanwhile.
type httpsOnlyRoundTripper struct {
	base http.RoundTripper
}

func (t httpsOnlyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("refusing non-HTTPS request to %q: the seed image registry must be served over HTTPS",
			req.URL.Redacted())
	}
	return t.base.RoundTrip(req)
}

// seedFormatFromConfig extracts and parses the lcav1.SeedFormatOCILabel value
// from an image config. It is split out so the label-parsing branches can be
// unit tested without a live registry.
func seedFormatFromConfig(imageRef string, cfg *v1.ConfigFile) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("seed image %q has no config", imageRef)
	}
	raw, ok := cfg.Config.Labels[lcav1.SeedFormatOCILabel]
	if !ok {
		return 0, fmt.Errorf("seed image %q has no %s label", imageRef, lcav1.SeedFormatOCILabel)
	}
	format, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("seed image %q has non-integer %s label %q: %w", imageRef, lcav1.SeedFormatOCILabel, raw, err)
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
	// name.NewRegistry canonicalises the Docker Hub alias "docker.io" to
	// "index.docker.io", so a Resource's RegistryStr() reports the canonical host.
	// Normalise the bare alias here too, otherwise a pull secret keyed on the
	// common "docker.io" form would never match a Docker Hub seed reference.
	if key == dockerHubAuthKeyAlias {
		return name.DefaultRegistry
	}
	return key
}
