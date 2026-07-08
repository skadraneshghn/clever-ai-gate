package quota

import (
	"sync"
	"sync/atomic"
)

// SlidingWindow tracks per-client token usage history using a circular buffer.
// It solves the "missing max_tokens" problem from IDE extensions like Cline
// that omit the field, causing naive gateways to assume worst-case quotas.
//
// Instead of guessing, we track actual usage and estimate future consumption
// based on historical averages with a 1.35x safety multiplier.
type SlidingWindow struct {
	history    []int64
	index      uint32
	windowSize uint32
	mu         sync.RWMutex
}

// NewSlidingWindow creates a new sliding window with the given capacity.
func NewSlidingWindow(windowSize int) *SlidingWindow {
	return &SlidingWindow{
		history:    make([]int64, windowSize),
		windowSize: uint32(windowSize),
	}
}

// EstimateMaxTokens calculates an estimated max_tokens value based on
// historical usage patterns.
//
// Algorithm:
//  1. Sum all non-zero entries in the circular buffer
//  2. Divide by count to get average
//  3. Multiply by 1.35 safety factor
//  4. Return fallback if no history exists
func (sw *SlidingWindow) EstimateMaxTokens(fallbackDefault int64) int64 {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	var total int64
	var count int64

	for _, val := range sw.history {
		if val > 0 {
			total += val
			count++
		}
	}

	if count == 0 {
		return fallbackDefault
	}

	average := total / count
	// Apply 1.35x safety multiplier to avoid underestimation
	return int64(float64(average) * 1.35)
}

// RecordUsage records actual token usage from a completed request.
// Uses atomic index increment for the circular buffer position.
func (sw *SlidingWindow) RecordUsage(tokens int64) {
	idx := atomic.AddUint32(&sw.index, 1) % sw.windowSize
	sw.mu.Lock()
	sw.history[idx] = tokens
	sw.mu.Unlock()
}

// Reset clears all history.
func (sw *SlidingWindow) Reset() {
	sw.mu.Lock()
	for i := range sw.history {
		sw.history[i] = 0
	}
	sw.mu.Unlock()
}

// AverageUsage returns the current average token usage (for metrics).
func (sw *SlidingWindow) AverageUsage() float64 {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	var total int64
	var count int64

	for _, val := range sw.history {
		if val > 0 {
			total += val
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}
