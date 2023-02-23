package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetParentPolicyNameAndNamespace(t *testing.T) {
	res, err := GetParentPolicyNameAndNamespace("default.upgrade")
	assert.NoError(t, err)
	assert.Equal(t, len(res), 2)

	res, err = GetParentPolicyNameAndNamespace("upgrade")
	assert.Error(t, err)

	res, err = GetParentPolicyNameAndNamespace("default.upgrade.cluster")
	assert.Equal(t, res[0], "default")
	assert.Equal(t, res[1], "upgrade.cluster")
}

func TestShouldSoak(t *testing.T) {
	// no annotation
	res, err := ShouldSoak(&unstructured.Unstructured{}, v1.Now())
	assert.Equal(t, res, false)
	assert.NoError(t, err)

	// annotation present and soak duration is not over
	policy := &unstructured.Unstructured{}
	policy.SetAnnotations(map[string]string{
		"soakSeconds": "10",
	})
	res, err = ShouldSoak(policy, v1.Now())
	assert.Equal(t, true, res)
	assert.NoError(t, err)

	// annotation present, firstCompliant is zero
	res, err = ShouldSoak(policy, v1.Time{})
	assert.Equal(t, true, res)
	assert.NoError(t, err)

	// annotation present and soak duration is over
	firstCompliantAt := time.Now().Add(time.Duration(-11) * time.Second)
	res, err = ShouldSoak(policy, v1.NewTime(firstCompliantAt))
	assert.Equal(t, false, res)
	assert.NoError(t, err)

	// annotation present, soak duration is invalid
	policy.SetAnnotations(map[string]string{
		"soakSeconds": "-1",
	})
	_, err = ShouldSoak(policy, v1.Now())
	assert.Error(t, err)
}
