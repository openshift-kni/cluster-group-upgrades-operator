package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSafeResourceNames(t *testing.T) {

	name := "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	safeName := GetSafeResourceName(name, MaxPolicyNameLength, len("ztp-install")+1)
	assert.Equal(t, MaxPolicyNameLength-len("ztp-install")-1, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-len("ztp-install")-7]+"-", safeName[:MaxPolicyNameLength-len("ztp-install")-6])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = GetSafeResourceName(name, MaxPolicyNameLength, 0)
	assert.Equal(t, MaxPolicyNameLength, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-7]+"-", safeName[:MaxPolicyNameLength-6])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = GetSafeResourceName(name, MaxObjectNameLength, 0)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-", safeName[:len(name)+1])
}
