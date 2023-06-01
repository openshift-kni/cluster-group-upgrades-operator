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
		SoakAnnotation: "10",
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
		SoakAnnotation: "-1",
	})
	_, err = ShouldSoak(policy, v1.Now())
	assert.Error(t, err)
}

func TestUpdateManagedPolicyNamespaceList(t *testing.T) {
	testcases := []struct {
		policiesNs     map[string][]string
		policyNameArr  []string
		expectedResult map[string][]string
	}{
		{
			policiesNs: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
			policyNameArr: []string{"bbb", "policy2"},
			expectedResult: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa", "bbb"},
			},
		},
		{
			policiesNs:    map[string][]string{},
			policyNameArr: []string{"bbb", "policy2"},
			expectedResult: map[string][]string{
				"policy2": {"bbb"},
			},
		},
		{
			policiesNs: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
			policyNameArr: []string{"aaa", "policy1"},
			expectedResult: map[string][]string{
				"policy1": {"aaa", "bbb"},
				"policy2": {"aaa"},
			},
		},
	}

	for _, tc := range testcases {
		UpdateManagedPolicyNamespaceList(tc.policiesNs, tc.policyNameArr)
		assert.Equal(t, tc.policiesNs, tc.expectedResult)
	}
}
