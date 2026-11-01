package descheduler

import (
	"runtime"
	"testing"
)

func TestReleaseIdleHeapForcesGC(t *testing.T) {
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	releaseIdleHeap()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.NumGC <= before.NumGC {
		t.Fatalf("releaseIdleHeap should force a garbage collection, NumGC before=%d after=%d", before.NumGC, after.NumGC)
	}
}
