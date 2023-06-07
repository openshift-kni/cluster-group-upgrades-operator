package controllers

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPrecache_parseSpaceRequired(t *testing.T) {
	testCases := []struct {
		name                     string
		spaceRequired            string
		expectedSpaceRequiredGiB string
		expectedError            bool
	}{
		{
			name:                     "invalid space required string",
			spaceRequired:            "abc 123",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "unknown space required format",
			spaceRequired:            "123 ab",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "negative space required value",
			spaceRequired:            "-1 GiB",
			expectedSpaceRequiredGiB: "",
			expectedError:            true,
		},
		{
			name:                     "convert byte to GiB",
			spaceRequired:            "1073741824",
			expectedSpaceRequiredGiB: "1",
			expectedError:            false,
		},
		{
			name:                     "convert KB to GiB",
			spaceRequired:            "2500000 KB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert MB to GiB",
			spaceRequired:            "3100 MB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert GB to GiB",
			spaceRequired:            "40 GB",
			expectedSpaceRequiredGiB: "38",
			expectedError:            false,
		},
		{
			name:                     "convert float-valued GB to GiB",
			spaceRequired:            "38.5 GB",
			expectedSpaceRequiredGiB: "36",
			expectedError:            false,
		},
		{
			name:                     "convert TB to GiB",
			spaceRequired:            "2 TB",
			expectedSpaceRequiredGiB: "1863",
			expectedError:            false,
		},
		{
			name:                     "convert PB to GiB",
			spaceRequired:            "1 PB",
			expectedSpaceRequiredGiB: "931323",
			expectedError:            false,
		},
		{
			name:                     "convert KiB to GiB",
			spaceRequired:            "2500000 KiB",
			expectedSpaceRequiredGiB: "3",
			expectedError:            false,
		},
		{
			name:                     "convert MiB to GiB",
			spaceRequired:            "3100 MiB",
			expectedSpaceRequiredGiB: "4",
			expectedError:            false,
		},
		{
			name:                     "convert GiB to GiB",
			spaceRequired:            "40 GiB",
			expectedSpaceRequiredGiB: "40",
			expectedError:            false,
		},
		{
			name:                     "convert float-valued GiB to GiB",
			spaceRequired:            "38.5 GiB",
			expectedSpaceRequiredGiB: "39",
			expectedError:            false,
		},
		{
			name:                     "convert TiB to GiB",
			spaceRequired:            "2 TiB",
			expectedSpaceRequiredGiB: "2048",
			expectedError:            false,
		},
		{
			name:                     "convert PiB to GiB",
			spaceRequired:            "1 PiB",
			expectedSpaceRequiredGiB: "1048576",
			expectedError:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsedSpaceRequired, err := parseSpaceRequired(tc.spaceRequired)
			if tc.expectedError {
				assert.NotEqual(t, nil, err)
			}
			assert.Equal(t, tc.expectedSpaceRequiredGiB, parsedSpaceRequired)
		})
	}
}
