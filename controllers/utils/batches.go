package utils

import (
	"time"
)

// CalculateBatchTimeout calculates the current batch timeout for the running cgu
func CalculateBatchTimeout(timeoutMinutes, numBatches, currentBatch int, currentBatchStartTime, cguStartTime time.Time) time.Duration {

	// The remaining time will be the total timeout subtract the elapsed time spent on both the batch and cgu
	// It is important to include the current batch here so that we don't let the entire timeout be consumed by a single batch that gets stuck
	remainingTime := float64(timeoutMinutes)*float64(time.Minute) - float64(currentBatchStartTime.Sub(cguStartTime).Nanoseconds())

	// The number of batches will always be at least 1, and currentBatch is indexed from 0, so there should never be a <1 result here
	remainingBatches := numBatches - currentBatch

	// The current batch's timeout shall be the remaining time divided by the number of batches remaining
	// This is to ensure we are giving each batch as much time as possible within the remaining allotment
	currentBatchTimeout := time.Duration(remainingTime / float64(remainingBatches))

	return currentBatchTimeout
}
