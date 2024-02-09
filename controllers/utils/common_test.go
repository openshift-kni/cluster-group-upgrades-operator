package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSafeResourceNames(t *testing.T) {

	name := "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	namespace := "ztp-install"
	safeName := NewSafeResourceName(name, namespace, "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "", MaxObjectNameLength)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-", safeName[:len(name)+1])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	safeName = NewSafeResourceName(name, namespace, "kuttl", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxObjectNameLength)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-kuttl", safeName)

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-5])

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4"
	safeName = NewSafeResourceName(name, "", "12345678", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-9]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-8])
}
