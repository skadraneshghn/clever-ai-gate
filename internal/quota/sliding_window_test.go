package quota

import (
	"sync"
	"testing"
)

func TestEstimateMaxTokens_NoHistory(t *testing.T) {
	sw := NewSlidingWindow(10)
	result := sw.EstimateMaxTokens(4096)
	if result != 4096 {
		t.Errorf("expected fallback 4096, got %d", result)
	}
}

func TestEstimateMaxTokens_WithHistory(t *testing.T) {
	sw := NewSlidingWindow(10)

	// Record some usage
	sw.RecordUsage(100)
	sw.RecordUsage(200)
	sw.RecordUsage(300)

	result := sw.EstimateMaxTokens(4096)
	// Average = 200, with 1.35x = 270
	if result != 270 {
		t.Errorf("expected 270, got %d", result)
	}
}

func TestRecordUsage_CircularBuffer(t *testing.T) {
	sw := NewSlidingWindow(3)

	// Fill the buffer
	sw.RecordUsage(100)
	sw.RecordUsage(200)
	sw.RecordUsage(300)

	// Overwrite oldest entry
	sw.RecordUsage(400)

	result := sw.EstimateMaxTokens(4096)
	// Active values should be 200, 300, 400 → avg 300, × 1.35 = 405
	if result != 405 {
		t.Errorf("expected 405, got %d", result)
	}
}

func TestReset(t *testing.T) {
	sw := NewSlidingWindow(5)
	sw.RecordUsage(1000)
	sw.RecordUsage(2000)

	sw.Reset()

	result := sw.EstimateMaxTokens(4096)
	if result != 4096 {
		t.Errorf("expected fallback after reset, got %d", result)
	}
}

func TestConcurrentRecordUsage(t *testing.T) {
	sw := NewSlidingWindow(100)

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(tokens int64) {
			defer wg.Done()
			sw.RecordUsage(tokens)
		}(int64(i * 10))
	}
	wg.Wait()

	// Should not panic and should return a reasonable estimate
	result := sw.EstimateMaxTokens(4096)
	if result <= 0 {
		t.Errorf("expected positive estimate, got %d", result)
	}
}

func BenchmarkRecordUsage(b *testing.B) {
	sw := NewSlidingWindow(100)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sw.RecordUsage(int64(i))
	}
}

func BenchmarkEstimateMaxTokens(b *testing.B) {
	sw := NewSlidingWindow(100)
	for i := 0; i < 100; i++ {
		sw.RecordUsage(int64(i * 100))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sw.EstimateMaxTokens(4096)
	}
}
