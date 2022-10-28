package cmd_test

import (
	"testing"

	"github.com/openshift-kni/cluster-group-upgrades-operator/recovery/cmd"
	"github.com/stretchr/testify/assert"
)

func TestCompare(t *testing.T) {

	testcases := []struct {
		estimated, freeDisk float64
		expected            bool
		name                string
	}{
		{
			estimated: 80.00 * 1024 * 1024 * 1024,
			freeDisk:  100.00 * 1024 * 1024 * 1024,
			expected:  true,
			name:      "higher disk space in same unit",
		},
		{
			estimated: 800.00 * 1024 * 1024 * 1024,
			freeDisk:  1.00 * 1024 * 1024 * 1024 * 1024,
			expected:  true,
			name:      "higher disk space in different unit",
		},
		{
			estimated: 800.00 * 1024 * 1024 * 1024,
			freeDisk:  800.00 * 1024 * 1024 * 1024,
			expected:  false,
			name:      "equal disk space",
		},
		{
			estimated: 80.00 * 1024 * 1024 * 1024,
			freeDisk:  50.00 * 1024 * 1024 * 1024,
			expected:  false,
			name:      "lower disk space in same unit",
		},
		{
			estimated: 80.00 * 1024 * 1024 * 1024,
			freeDisk:  100.00 * 1024 * 1024,
			expected:  false,
			name:      "lower disk space in different unit",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := cmd.Compare(tc.freeDisk, tc.estimated)
			assert.Equal(t, tc.expected, actual, "The expected and actual value should be the same.")
		})
	}

}

func TestSizeConversion(t *testing.T) {

	type convertedSizeExpected struct {
		number float64
		unit   string
	}

	type convertedSizeActual struct {
		number float64
		unit   string
	}

	testcases := []struct {
		size     float64
		expected convertedSizeExpected
		name     string
	}{
		{
			size: 10.00 * 1024,
			expected: convertedSizeExpected{
				number: 10.00,
				unit:   "KiB",
			},
			name: "KiB conversion",
		},
		{
			size: 20.00 * 1024 * 1024,
			expected: convertedSizeExpected{
				number: 20.00,
				unit:   "MiB",
			},
			name: "MiB conversion",
		},
		{
			size: 40.00 * 1024 * 1024 * 1024,
			expected: convertedSizeExpected{
				number: 40.00,
				unit:   "GiB",
			},
			name: "GiB conversion",
		},

		{
			size: 80.00 * 1024 * 1024 * 1024 * 1024,
			expected: convertedSizeExpected{
				number: 80.00,
				unit:   "TiB",
			},
			name: "TiB conversion",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actualNumber, actualSize := cmd.SizeConversion(tc.size)
			actual := convertedSizeActual{
				number: actualNumber,
				unit:   actualSize,
			}
			assert.Equal(t, tc.expected.unit, actual.unit, "The expected and actual unit must be the same.")
			assert.Equal(t, tc.expected.number, actual.number, "The expected and actual number must be the same.")
		})
	}
}
