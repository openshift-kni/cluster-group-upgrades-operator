package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSafeResourceNames(t *testing.T) {

	name := "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	namespace := "ztp-install"
	safeName := NewSafeResourceName(name, namespace, "", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-len(namespace)-6]+"-", safeName[:MaxPolicyNameLength-len(namespace)-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-6]+"-", safeName[:MaxPolicyNameLength-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "", MaxObjectNameLength, nil)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-", safeName[:len(name)+1])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	safeName = NewSafeResourceName(name, namespace, "kuttl", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-len(namespace)-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxObjectNameLength, nil)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-kuttl", safeName)

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-6]+"-", safeName[:MaxPolicyNameLength-5])

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4"
	safeName = NewSafeResourceName(name, "", "12345678", MaxPolicyNameLength, nil)
	assert.Equal(t, MaxPolicyNameLength, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLength-9]+"-", safeName[:MaxPolicyNameLength-8])
}
