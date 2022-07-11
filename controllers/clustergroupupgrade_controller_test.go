/*
Copyright 2022.

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

package controllers

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
				currentBatch:          0,
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
				currentBatch:          0,
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
				currentBatch:          1,
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
				currentBatch:          0,
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
				currentBatch:          1,
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
				currentBatch:          4,
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
				currentBatch:          99,
			},
			expected: time.Duration(50 * time.Minute),
			name:     "Edge case 1",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := calculateBatchTimeout(
				tc.inputs.timeoutMinutes,
				tc.inputs.currentBatchStartTime,
				tc.inputs.cguStartTime,
				tc.inputs.numBatches,
				tc.inputs.currentBatch)
			assert.Equal(t, tc.expected, actual, "The expected and actual timeout should be the same.")
		})
	}
}
