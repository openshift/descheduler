/*
Copyright 2026 The Kubernetes Authors.

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

package kubevirtmigrationaware

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// makeVMI builds an unstructured VMI object with optional migrationState.
// migrationState may be nil (no migration ever ran), a map with only
// "startTimestamp" (in progress), or a map with both timestamps (completed).
// No kubevirt imports are needed: the object is just a plain nested map.
func makeVMI(namespace, name string, migrationState map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
	if migrationState != nil {
		obj.Object["status"] = map[string]interface{}{
			"migrationState": migrationState,
		}
	}
	return obj
}

// inProgressState returns a migrationState map representing an ongoing migration.
func inProgressState(start time.Time) map[string]interface{} {
	return map[string]interface{}{
		"startTimestamp": start.UTC().Format(time.RFC3339),
	}
}

// completedState returns a migrationState map representing a finished migration.
func completedState(start, end time.Time) map[string]interface{} {
	return map[string]interface{}{
		"startTimestamp": start.UTC().Format(time.RFC3339),
		"endTimestamp":   end.UTC().Format(time.RFC3339),
	}
}

// makeVirtLauncherPod returns a pod that carries the kubevirt.io/domain annotation
// linking it to a VMI, as a real virt-launcher pod would.
func makeVirtLauncherPod(namespace, name, nodeName, vmiName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				vmiAnnotationKey: vmiName,
			},
		},
		Spec: v1.PodSpec{NodeName: nodeName},
	}
}

// makePlainPod returns a pod with no kubevirt annotation (e.g. a regular workload).
func makePlainPod(namespace, name, nodeName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       v1.PodSpec{NodeName: nodeName},
	}
}

// makeVMILister builds a cache.GenericLister pre-populated with the given VMIs.
// It mirrors exactly what the production dynamic informer would serve, without
// any dynamic client or network calls.
func makeVMILister(vmis ...*unstructured.Unstructured) cache.GenericLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, vmi := range vmis {
		_ = indexer.Add(vmi)
	}
	return cache.NewGenericLister(indexer, vmiGVR.GroupResource())
}

// newTestPlugin is a convenience wrapper that calls the internal constructor
// with a fake lister.  Pass maxCooldown=0 to disable the adaptive cap.
func newTestPlugin(t *testing.T, cooldown, maxCooldown time.Duration, vmis ...*unstructured.Unstructured) *KubevirtMigrationAware {
	t.Helper()
	args := &KubevirtMigrationAwareArgs{
		MigrationCooldown:      metav1.Duration{Duration: cooldown},
		MaxMigrationCooldown:   metav1.Duration{Duration: maxCooldown},
		MigrationHistoryWindow: metav1.Duration{Duration: defaultMigrationHistoryWindow},
	}
	pg, err := newPlugin(context.Background(), args, nil, makeVMILister(vmis...), newMigrationHistory())
	if err != nil {
		t.Fatalf("newPlugin: %v", err)
	}
	return pg.(*KubevirtMigrationAware)
}

// ── Filter ────────────────────────────────────────────────────────────────────

func TestFilter(t *testing.T) {
	const ns = "default"
	now := time.Now()

	cases := []struct {
		description string
		vmis        []*unstructured.Unstructured
		pod         *v1.Pod
		wantAllow   bool
	}{
		{
			description: "non-virt-launcher pod (no annotation) is always allowed",
			pod:         makePlainPod(ns, "plain-pod", "node-1"),
			wantAllow:   true,
		},
		{
			description: "virt-launcher pod whose VMI is absent from cache is allowed (fail open)",
			pod:         makeVirtLauncherPod(ns, "virt-launcher-a", "node-1", "vm-a"),
			// no VMIs added to the lister
			wantAllow: true,
		},
		{
			description: "virt-launcher pod whose VMI has no migration history is allowed",
			vmis:        []*unstructured.Unstructured{makeVMI(ns, "vm-b", nil)},
			pod:         makeVirtLauncherPod(ns, "virt-launcher-b", "node-1", "vm-b"),
			wantAllow:   true,
		},
		{
			description: "virt-launcher pod whose VMI has a completed migration is allowed",
			vmis: []*unstructured.Unstructured{
				makeVMI(ns, "vm-c", completedState(now.Add(-10*time.Minute), now.Add(-5*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-c", "node-2", "vm-c"),
			wantAllow: true,
		},
		{
			description: "virt-launcher pod whose VMI migration is in progress is blocked",
			vmis: []*unstructured.Unstructured{
				makeVMI(ns, "vm-d", inProgressState(now.Add(-2*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-d", "node-1", "vm-d"),
			wantAllow: false,
		},
		{
			description: "pod in different namespace from VMI cache entry is allowed (cache miss)",
			vmis: []*unstructured.Unstructured{
				makeVMI("other-ns", "vm-e", inProgressState(now.Add(-1*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-e", "node-1", "vm-e"),
			wantAllow: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			plugin := newTestPlugin(t, 5*time.Minute, 0, tc.vmis...)
			got := plugin.Filter(tc.pod)
			if got != tc.wantAllow {
				t.Errorf("Filter() = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}

// ── PreEvictionFilter ─────────────────────────────────────────────────────────

func TestPreEvictionFilter(t *testing.T) {
	const (
		ns       = "default"
		cooldown = 5 * time.Minute
	)
	now := time.Now()

	cases := []struct {
		description string
		vmis        []*unstructured.Unstructured
		pod         *v1.Pod
		wantAllow   bool
	}{
		{
			description: "non-virt-launcher pod is always allowed",
			pod:         makePlainPod(ns, "plain-pod", "node-1"),
			wantAllow:   true,
		},
		{
			description: "VMI absent from cache is allowed (fail open)",
			pod:         makeVirtLauncherPod(ns, "virt-launcher-a", "node-1", "vm-a"),
			wantAllow:   true,
		},
		{
			description: "VMI with no migration history is allowed",
			vmis:        []*unstructured.Unstructured{makeVMI(ns, "vm-b", nil)},
			pod:         makeVirtLauncherPod(ns, "virt-launcher-b", "node-1", "vm-b"),
			wantAllow:   true,
		},
		{
			description: "VMI whose migration ended just within the cooldown window is deferred",
			vmis: []*unstructured.Unstructured{
				// ended 1 minute ago; cooldown is 5 minutes → still blocked
				makeVMI(ns, "vm-c", completedState(now.Add(-10*time.Minute), now.Add(-1*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-c", "node-2", "vm-c"),
			wantAllow: false,
		},
		{
			description: "VMI whose migration ended exactly at the cooldown boundary is allowed",
			vmis: []*unstructured.Unstructured{
				// ended 5 minutes + 1 second ago → just past the cooldown
				makeVMI(ns, "vm-d", completedState(now.Add(-10*time.Minute), now.Add(-(cooldown+time.Second)))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-d", "node-2", "vm-d"),
			wantAllow: true,
		},
		{
			description: "VMI whose migration ended well before the cooldown window is allowed",
			vmis: []*unstructured.Unstructured{
				makeVMI(ns, "vm-e", completedState(now.Add(-30*time.Minute), now.Add(-20*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-e", "node-3", "vm-e"),
			wantAllow: true,
		},
		{
			description: "VMI mid-migration has no endTimestamp so is allowed by PreEvictionFilter (Filter handles this)",
			vmis: []*unstructured.Unstructured{
				makeVMI(ns, "vm-f", inProgressState(now.Add(-2*time.Minute))),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-f", "node-1", "vm-f"),
			wantAllow: true,
		},
		{
			description: "malformed endTimestamp is treated as no timestamp (fail open)",
			vmis: []*unstructured.Unstructured{
				makeVMI(ns, "vm-g", map[string]interface{}{
					"startTimestamp": now.Add(-10 * time.Minute).UTC().Format(time.RFC3339),
					"endTimestamp":   "not-a-valid-timestamp",
				}),
			},
			pod:       makeVirtLauncherPod(ns, "virt-launcher-g", "node-1", "vm-g"),
			wantAllow: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			plugin := newTestPlugin(t, cooldown, 0, tc.vmis...)
			got := plugin.PreEvictionFilter(tc.pod)
			if got != tc.wantAllow {
				t.Errorf("PreEvictionFilter() = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}

// ── Cooldown duration is respected ───────────────────────────────────────────

func TestPreEvictionFilterRespectsConfiguredCooldown(t *testing.T) {
	const ns = "default"
	now := time.Now()
	endedAgo := 3 * time.Minute

	// A 1-minute migration ensures the configured cooldown always dominates
	// (migration duration < any cooldown under test).
	vmi := makeVMI(ns, "vm-1", completedState(now.Add(-(endedAgo+time.Minute)), now.Add(-endedAgo)))
	pod := makeVirtLauncherPod(ns, "virt-launcher-1", "node-1", "vm-1")

	// With a 5-minute cooldown the VM (ended 3m ago) should be deferred.
	t.Run("5m cooldown blocks VM ended 3m ago", func(t *testing.T) {
		plugin := newTestPlugin(t, 5*time.Minute, 0, vmi)
		if plugin.PreEvictionFilter(pod) {
			t.Error("PreEvictionFilter() = true (allowed), want false (deferred)")
		}
	})

	// With a 2-minute cooldown the same VM should be evictable (elapsed 3m > effective 2m).
	t.Run("2m cooldown allows VM ended 3m ago", func(t *testing.T) {
		plugin := newTestPlugin(t, 2*time.Minute, 0, vmi)
		if !plugin.PreEvictionFilter(pod) {
			t.Error("PreEvictionFilter() = false (deferred), want true (allowed)")
		}
	})

	// With zero cooldown the adaptive cooldown equals the migration duration (1m);
	// elapsed 3m > 1m so the VM is immediately evictable.
	t.Run("zero cooldown allows VM ended 3m ago (1m migration)", func(t *testing.T) {
		plugin := newTestPlugin(t, 0, 0, vmi)
		if !plugin.PreEvictionFilter(pod) {
			t.Error("PreEvictionFilter() = false (deferred), want true (allowed)")
		}
	})
}

// ── Adaptive per-VM cooldown ──────────────────────────────────────────────────

func TestPreEvictionFilterAdaptiveCooldown(t *testing.T) {
	const (
		ns         = "default"
		configured = 15 * time.Minute
	)
	now := time.Now()

	cases := []struct {
		description   string
		migStart      time.Duration // relative to now
		migEnd        time.Duration // relative to now
		maxCooldown   time.Duration // 0 = disabled
		wantAllow     bool
	}{
		{
			// Small VM: 2-minute migration — configured 15m dominates.
			// Ended 16m ago → elapsed(16m) > effective(15m) → allowed.
			description: "small VM: configured cooldown dominates, elapsed past it",
			migStart:    -20 * time.Minute,
			migEnd:      -18 * time.Minute, // duration = 2m
			wantAllow:   true,
		},
		{
			// Small VM: same 2-minute migration, but ended only 10m ago.
			// effective = max(15m, 2m) = 15m; elapsed(10m) < 15m → blocked.
			description: "small VM: configured cooldown dominates, still within window",
			migStart:    -25 * time.Minute,
			migEnd:      -10 * time.Minute, // duration = 15m; elapsed = 10m
			wantAllow:   false,
		},
		{
			// Large VM: 30-minute migration — duration dominates over 15m.
			// Ended 5m ago → elapsed(5m) < effective(30m) → blocked.
			description: "large VM: migration duration dominates, still within window",
			migStart:    -35 * time.Minute,
			migEnd:      -5 * time.Minute, // duration = 30m
			wantAllow:   false,
		},
		{
			// Large VM: 30-minute migration, ended 31m ago.
			// effective = 30m; elapsed(31m) > 30m → allowed.
			description: "large VM: migration duration dominates, elapsed past it",
			migStart:    -61 * time.Minute,
			migEnd:      -31 * time.Minute, // duration = 30m
			wantAllow:   true,
		},
		{
			// Large VM with cap: 30-minute migration capped at 20m.
			// effective = min(max(15m, 30m), 20m) = 20m; ended 5m ago → blocked.
			description: "large VM: cap applied, still within capped window",
			migStart:    -35 * time.Minute,
			migEnd:      -5 * time.Minute, // duration = 30m; cap = 20m
			maxCooldown: 20 * time.Minute,
			wantAllow:   false,
		},
		{
			// Large VM with cap: same migration, but ended 21m ago.
			// effective = 20m (capped); elapsed(21m) > 20m → allowed.
			description: "large VM: cap applied, elapsed past capped window",
			migStart:    -56 * time.Minute,
			migEnd:      -21 * time.Minute, // duration = 35m; cap = 20m
			maxCooldown: 20 * time.Minute,
			wantAllow:   true,
		},
		{
			// No startTimestamp: adaptive path skipped, falls back to configured cooldown.
			// effective = 15m; ended 10m ago → blocked.
			description: "missing startTimestamp: falls back to configured cooldown",
			migStart:    0, // sentinel: will be omitted from migrationState
			migEnd:      -10 * time.Minute,
			wantAllow:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			var state map[string]interface{}
			if tc.migStart == 0 {
				// Only endTimestamp, no startTimestamp.
				state = map[string]interface{}{
					"endTimestamp": now.Add(tc.migEnd).UTC().Format(time.RFC3339),
				}
			} else {
				state = completedState(now.Add(tc.migStart), now.Add(tc.migEnd))
			}
			vmi := makeVMI(ns, "vm-adaptive", state)
			pod := makeVirtLauncherPod(ns, "virt-launcher-adaptive", "node-1", "vm-adaptive")

			plugin := newTestPlugin(t, configured, tc.maxCooldown, vmi)
			got := plugin.PreEvictionFilter(pod)
			if got != tc.wantAllow {
				t.Errorf("PreEvictionFilter() = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}

// ── Exponential backoff from migration frequency ──────────────────────────────

func TestPreEvictionFilterExponentialBackoff(t *testing.T) {
	const (
		ns          = "default"
		cooldown    = 15 * time.Minute
		vmiUID      = types.UID("uid-vm-backoff")
	)
	now := time.Now()

	// A 10-second migration that ended 5 minutes ago.
	// Base effective cooldown = max(15m, 10s) = 15m.  elapsed = 5m < 15m.
	vmi := makeVMI(ns, "vm-backoff", completedState(
		now.Add(-5*time.Minute-10*time.Second),
		now.Add(-5*time.Minute),
	))
	vmi.SetUID(vmiUID)
	pod := makeVirtLauncherPod(ns, "virt-launcher-backoff", "node-1", "vm-backoff")

	cases := []struct {
		description  string
		historyCount int           // entries to pre-populate (spread over last few hours)
		maxCooldown  time.Duration // 0 = disabled
		wantAllow    bool
	}{
		{
			// count=0: race — current migration not yet in history; base 15m applies.
			description:  "count=0 (race): base cooldown, blocked",
			historyCount: 0,
			wantAllow:    false, // 15m * 2^0 = 15m; elapsed 5m < 15m
		},
		{
			// count=1: one entry (current migration recorded); 2^0 doublings = 15m.
			description:  "count=1: first migration, base cooldown, blocked",
			historyCount: 1,
			wantAllow:    false, // 15m * 2^0 = 15m; elapsed 5m < 15m
		},
		{
			// count=2: one prior + current; 2^1 doublings → 30m.
			description:  "count=2: one prior migration doubles cooldown to 30m, blocked",
			historyCount: 2,
			wantAllow:    false, // 15m * 2^1 = 30m; elapsed 5m < 30m
		},
		{
			// count=3: two prior; 2^2 doublings → 60m.
			description:  "count=3: two prior migrations, cooldown 60m, blocked",
			historyCount: 3,
			wantAllow:    false, // 15m * 2^2 = 60m; elapsed 5m < 60m
		},
		{
			// count=2 with max=20m: min(30m, 20m) = 20m; elapsed 5m < 20m → blocked.
			description:  "count=2 with max cap at 20m: capped, still blocked",
			historyCount: 2,
			maxCooldown:  20 * time.Minute,
			wantAllow:    false,
		},
		{
			// count=4 with max=20m: min(120m, 20m) = 20m; elapsed 5m < 20m → blocked.
			description:  "count=4 with max cap at 20m: cap bounds growth, blocked",
			historyCount: 4,
			maxCooldown:  20 * time.Minute,
			wantAllow:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			plugin := newTestPlugin(t, cooldown, tc.maxCooldown, vmi)
			// Pre-populate history: spread entries so they all fall within the 6h window.
			for i := 0; i < tc.historyCount; i++ {
				plugin.history.record(vmiUID, now.Add(-time.Duration(i+1)*30*time.Minute))
			}
			got := plugin.PreEvictionFilter(pod)
			if got != tc.wantAllow {
				t.Errorf("PreEvictionFilter() = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}

// ── migrationHistory unit tests ───────────────────────────────────────────────

func TestMigrationHistory(t *testing.T) {
	const (
		uid    = types.UID("uid-test")
		window = 24 * time.Hour
	)
	now := time.Now()

	t.Run("empty history returns zero", func(t *testing.T) {
		h := newMigrationHistory()
		if got := h.countAndPrune(uid, window); got != 0 {
			t.Errorf("countAndPrune() = %d, want 0", got)
		}
	})

	t.Run("entries within window are counted", func(t *testing.T) {
		h := newMigrationHistory()
		h.record(uid, now.Add(-1*time.Hour))
		h.record(uid, now.Add(-2*time.Hour))
		if got := h.countAndPrune(uid, window); got != 2 {
			t.Errorf("countAndPrune() = %d, want 2", got)
		}
	})

	t.Run("entries outside the window are pruned", func(t *testing.T) {
		h := newMigrationHistory()
		h.record(uid, now.Add(-25*time.Hour)) // outside 24h window
		h.record(uid, now.Add(-1*time.Hour))  // inside
		if got := h.countAndPrune(uid, window); got != 1 {
			t.Errorf("countAndPrune() = %d, want 1 (stale entry pruned)", got)
		}
	})

	t.Run("all entries expired: map entry is deleted", func(t *testing.T) {
		h := newMigrationHistory()
		h.record(uid, now.Add(-25*time.Hour))
		if got := h.countAndPrune(uid, window); got != 0 {
			t.Errorf("countAndPrune() = %d, want 0", got)
		}
		h.mu.Lock()
		_, exists := h.completions[uid]
		h.mu.Unlock()
		if exists {
			t.Error("map entry was not deleted after all entries expired")
		}
	})
}

func TestMigrationHistoryOnVMIUpdate(t *testing.T) {
	const (
		ns  = "default"
		uid = types.UID("uid-update-test")
	)
	now := time.Now()

	withUID := func(vmi *unstructured.Unstructured) *unstructured.Unstructured {
		vmi.SetUID(uid)
		return vmi
	}

	t.Run("migration completion is recorded", func(t *testing.T) {
		h := newMigrationHistory()
		old := withUID(makeVMI(ns, "vmi", inProgressState(now.Add(-10*time.Minute))))
		new := withUID(makeVMI(ns, "vmi", completedState(now.Add(-10*time.Minute), now.Add(-1*time.Minute))))
		h.onVMIUpdate(old, new)
		if got := h.countAndPrune(uid, 24*time.Hour); got != 1 {
			t.Errorf("countAndPrune() = %d, want 1", got)
		}
	})

	t.Run("update with unchanged endTimestamp is not re-recorded", func(t *testing.T) {
		h := newMigrationHistory()
		vmi := withUID(makeVMI(ns, "vmi", completedState(now.Add(-10*time.Minute), now.Add(-1*time.Minute))))
		h.onVMIUpdate(vmi, vmi) // same object, endTimestamp unchanged
		if got := h.countAndPrune(uid, 24*time.Hour); got != 0 {
			t.Errorf("countAndPrune() = %d, want 0 (no transition)", got)
		}
	})

	t.Run("update with no migration state is ignored", func(t *testing.T) {
		h := newMigrationHistory()
		old := withUID(makeVMI(ns, "vmi", nil))
		new := withUID(makeVMI(ns, "vmi", nil))
		h.onVMIUpdate(old, new)
		if got := h.countAndPrune(uid, 24*time.Hour); got != 0 {
			t.Errorf("countAndPrune() = %d, want 0", got)
		}
	})

	t.Run("second migration completion increments count", func(t *testing.T) {
		h := newMigrationHistory()
		first := withUID(makeVMI(ns, "vmi", completedState(now.Add(-3*time.Hour), now.Add(-2*time.Hour))))
		second := withUID(makeVMI(ns, "vmi", completedState(now.Add(-30*time.Minute), now.Add(-5*time.Minute))))
		h.onVMIUpdate(first, second)
		if got := h.countAndPrune(uid, 24*time.Hour); got != 1 {
			t.Errorf("countAndPrune() = %d, want 1", got)
		}
	})
}
