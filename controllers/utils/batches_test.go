package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBatchTimeout(t *testing.T) {

	type (
		BatchTimeoutTestInputs struct {
			timeoutMinutes        int
			currentBatchStartTime time.Time
			cguStartTime          time.Time
			numBatches            int
			currentBatch          int
		}
	)

	testcases := []struct {
		inputs   BatchTimeoutTestInputs
		expected time.Duration
		name     string
	}{
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        100,
				currentBatchStartTime: time.Unix(1657000000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            1,
				currentBatch:          1,
			},
			expected: time.Duration(100 * time.Minute),
			name:     "Base case 1",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        200,
				currentBatchStartTime: time.Unix(1657000000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            2,
				currentBatch:          1,
			},
			expected: time.Duration(100 * time.Minute),
			name:     "Base case 2",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        200,
				currentBatchStartTime: time.Unix(1657006000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            2,
				currentBatch:          2,
			},
			expected: time.Duration(100 * time.Minute),
			name:     "Base case 3",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        240,
				currentBatchStartTime: time.Unix(1657000000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            5,
				currentBatch:          1,
			},
			expected: time.Duration(48 * time.Minute),
			name:     "Realistic case 1",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        240,
				currentBatchStartTime: time.Unix(1657002400, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            5,
				currentBatch:          2,
			},
			expected: time.Duration(50 * time.Minute),
			name:     "Realistic case 2",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        240,
				currentBatchStartTime: time.Unix(1657006000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            5,
				currentBatch:          5,
			},
			expected: time.Duration(140 * time.Minute),
			name:     "Realistic case 3",
		},
		{
			inputs: BatchTimeoutTestInputs{
				timeoutMinutes:        100,
				currentBatchStartTime: time.Unix(1657003000, 0),
				cguStartTime:          time.Unix(1657000000, 0),
				numBatches:            100,
				currentBatch:          100,
			},
			expected: time.Duration(50 * time.Minute),
			name:     "Edge case 1",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := CalculateBatchTimeout(
				tc.inputs.timeoutMinutes,
				tc.inputs.numBatches,
				tc.inputs.currentBatch,
				tc.inputs.currentBatchStartTime,
				tc.inputs.cguStartTime)
			assert.Equal(t, tc.expected, actual, "The expected and actual timeout should be the same.")
		})
	}
}
