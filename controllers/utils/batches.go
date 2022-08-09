package utils

import (
	"time"
)

// CalculateBatchTimeout calculates the current batch timeout for the running cgu
func CalculateBatchTimeout(timeoutMinutes, numBatches, currentBatch int, currentBatchStartTime, cguStartTime time.Time) time.Duration {

	// The remaining time will be the total timeout subtract the elapsed time spent on both the batch and cgu
	// It is important to include the current batch here so that we don't let the entire timeout be consumed by a single batch that gets stuck
	remainingTime := float64(timeoutMinutes)*float64(time.Minute) - float64(currentBatchStartTime.Sub(cguStartTime).Nanoseconds())

	// Because the current batch index automatically advances from 0 to 1 on the first loop,
	// then we need to subtract one when using it in here to ensure the math is correct
	remainingBatches := numBatches - (currentBatch - 1)

	// If this is the last batch then just return all the remaining time.
	// This also makes sure there is no division by zero below.
	if remainingBatches <= 1 {
		return time.Duration(remainingTime)
	}

	// The current batch's timeout shall be the remaining time divided by the number of batches remaining
	// This is to ensure we are giving each batch as much time as possible within the remaining allotment
	currentBatchTimeout := time.Duration(remainingTime / float64(remainingBatches))

	return currentBatchTimeout
}
