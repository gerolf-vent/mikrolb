package controller

import (
	"net/netip"
	"testing"
	"time"

	"github.com/gerolf-vent/mikrolb/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExpectations_StageAllocation_Basic(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	alloc := IPAllocationPending{
		ID:          "alloc-1",
		Address:     netip.MustParseAddr("10.0.0.1"),
		AllocatedAt: time.Now(),
	}

	if !e.StageAllocation(alloc) {
		t.Fatal("expected first StageAllocation to succeed")
	}
}

func TestExpectations_StageAllocation_SameIDIsIdempotent(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	alloc := IPAllocationPending{
		ID:          "alloc-1",
		Address:     netip.MustParseAddr("10.0.0.1"),
		AllocatedAt: time.Now(),
	}

	if !e.StageAllocation(alloc) {
		t.Fatal("first stage should succeed")
	}
	if !e.StageAllocation(alloc) {
		t.Fatal("re-staging the same ID for the same address should succeed")
	}
}

func TestExpectations_StageAllocation_DifferentIDSameAddressBlocked(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")

	alloc1 := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: time.Now()}
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: time.Now()}

	if !e.StageAllocation(alloc1) {
		t.Fatal("first allocation should succeed")
	}
	if e.StageAllocation(alloc2) {
		t.Fatal("second allocation for the same address with different ID should be blocked")
	}
}

func TestExpectations_StageAllocation_DifferentAddressesOK(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	alloc1 := IPAllocationPending{ID: "alloc-1", Address: netip.MustParseAddr("10.0.0.1"), AllocatedAt: time.Now()}
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: netip.MustParseAddr("10.0.0.2"), AllocatedAt: time.Now()}

	if !e.StageAllocation(alloc1) {
		t.Fatal("first allocation should succeed")
	}
	if !e.StageAllocation(alloc2) {
		t.Fatal("allocation for a different address should succeed")
	}
}

func TestExpectations_UnstageAllocation_Basic(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	now := time.Now()
	alloc := IPAllocationPending{ID: "alloc-1", Address: netip.MustParseAddr("10.0.0.1"), AllocatedAt: now}

	e.StageAllocation(alloc)
	e.UnstageAllocation(alloc)

	// After unstaging, a different ID should be able to claim the address
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: netip.MustParseAddr("10.0.0.1"), AllocatedAt: now}
	if !e.StageAllocation(alloc2) {
		t.Fatal("after unstaging, the address should be available for a different allocation")
	}
}

func TestExpectations_UnstageAllocation_OnlyMatchingIDAndTime(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	now := time.Now()
	addr := netip.MustParseAddr("10.0.0.1")
	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now}

	e.StageAllocation(alloc)

	// Try to unstage with a different time — should not remove it
	staleUnstage := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now.Add(-1 * time.Second)}
	e.UnstageAllocation(staleUnstage)

	// The original should still be blocking
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: now}
	if e.StageAllocation(alloc2) {
		t.Fatal("unstage with wrong time should not have cleared the pending allocation")
	}
}

func TestExpectations_UnstageAllocation_WrongIDNoOp(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now}
	e.StageAllocation(alloc)

	wrong := IPAllocationPending{ID: "alloc-wrong", Address: addr, AllocatedAt: now}
	e.UnstageAllocation(wrong)

	// Original should still be blocking
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: now}
	if e.StageAllocation(alloc2) {
		t.Fatal("unstage with wrong ID should not have cleared the pending allocation")
	}
}

func TestExpectations_UnstageAllocation_NonExistentIsNoOp(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	// Should not panic
	e.UnstageAllocation(IPAllocationPending{
		ID:          "does-not-exist",
		Address:     netip.MustParseAddr("10.0.0.1"),
		AllocatedAt: time.Now(),
	})
}

func TestExpectations_StageRelease_Basic(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)
	addr := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now}
	e.StageAllocation(alloc)
	e.StageRelease(addr, now.Add(1*time.Second))

	// After release, a different allocation should succeed
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: now.Add(2 * time.Second)}
	if !e.StageAllocation(alloc2) {
		t.Fatal("address should be available after release")
	}
}

func TestExpectations_StageRelease_IgnoredWhenNewerAllocationExists(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)
	addr := netip.MustParseAddr("10.0.0.1")

	allocTime := time.Now()
	releaseTime := allocTime.Add(-1 * time.Second) // release requested before the allocation

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: allocTime}
	e.StageAllocation(alloc)
	e.StageRelease(addr, releaseTime)

	// Allocation should still be blocking because it was created after the release
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: allocTime.Add(1 * time.Second)}
	if e.StageAllocation(alloc2) {
		t.Fatal("release before allocation should not clear the pending allocation")
	}
}

func TestExpectations_UnstageRelease_Basic(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)
	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now()

	e.StageRelease(addr, releaseTime)
	e.UnstageRelease(addr, releaseTime)

	// Verify via Resolve: the release should no longer remove the address from used IPs
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("cached-alloc", addr, releaseTime.Add(-1*time.Second)),
	}
	used := e.Resolve(cached, "")
	if _, exists := used[addr]; !exists {
		t.Fatal("after unstaging release, the cached allocation should still appear in used IPs")
	}
}

func TestExpectations_UnstageRelease_WrongTimeNoOp(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)
	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now()

	e.StageRelease(addr, releaseTime)
	e.UnstageRelease(addr, releaseTime.Add(1*time.Second)) // wrong time

	// Release should still be staged, so a cached allocation before release time
	// should be removed from used IPs
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("cached-alloc", addr, releaseTime.Add(-1*time.Second)),
	}
	used := e.Resolve(cached, "")
	if _, exists := used[addr]; exists {
		t.Fatal("unstage with wrong time should not have cleared the pending release")
	}
}

func TestExpectations_Resolve_CacheOnly(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	now := time.Now()
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", netip.MustParseAddr("10.0.0.1"), now),
		makeAllocWithAddress("alloc-2", netip.MustParseAddr("10.0.0.2"), now),
	}

	used := e.Resolve(cached, "")
	if len(used) != 2 {
		t.Fatalf("expected 2 used IPs, got %d", len(used))
	}
	if _, ok := used[netip.MustParseAddr("10.0.0.1")]; !ok {
		t.Fatal("expected 10.0.0.1 in used IPs")
	}
	if _, ok := used[netip.MustParseAddr("10.0.0.2")]; !ok {
		t.Fatal("expected 10.0.0.2 in used IPs")
	}
}

func TestExpectations_Resolve_ExcludesSelf(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	now := time.Now()
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-self", netip.MustParseAddr("10.0.0.1"), now),
		makeAllocWithAddress("alloc-other", netip.MustParseAddr("10.0.0.2"), now),
	}

	// Pending allocation for self should be excluded
	selfAlloc := IPAllocationPending{ID: "alloc-self", Address: netip.MustParseAddr("10.0.0.5"), AllocatedAt: now}
	e.StageAllocation(selfAlloc)

	used := e.Resolve(cached, "alloc-self")
	if _, ok := used[netip.MustParseAddr("10.0.0.5")]; ok {
		t.Fatal("pending allocation for excluded ID should not appear in used IPs")
	}
	if _, ok := used[netip.MustParseAddr("10.0.0.2")]; !ok {
		t.Fatal("other allocation should still appear in used IPs")
	}
}

func TestExpectations_Resolve_PendingAllocationApplied(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.5")
	now := time.Now()

	alloc := IPAllocationPending{ID: "alloc-new", Address: addr, AllocatedAt: now}
	e.StageAllocation(alloc)

	used := e.Resolve(nil, "")
	if _, ok := used[addr]; !ok {
		t.Fatal("pending allocation should appear in used IPs")
	}
}

func TestExpectations_Resolve_PendingReleaseRemovesCachedAllocation(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	allocTime := time.Now()
	releaseTime := allocTime.Add(1 * time.Second)

	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, allocTime),
	}

	e.StageRelease(addr, releaseTime)

	used := e.Resolve(cached, "")
	if _, ok := used[addr]; ok {
		t.Fatal("pending release should remove the cached allocation from used IPs")
	}
}

func TestExpectations_Resolve_PendingReleaseDoesNotRemoveNewerAllocation(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now()
	newerAllocTime := releaseTime.Add(1 * time.Second)

	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, newerAllocTime),
	}

	e.StageRelease(addr, releaseTime)

	used := e.Resolve(cached, "")
	if _, ok := used[addr]; !ok {
		t.Fatal("pending release should not remove a newer cached allocation from used IPs")
	}
}

func TestExpectations_Resolve_SkipsAllocationsWithoutAddress(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	cached := []v1alpha1.IPAllocation{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "alloc-pending"},
			Status: v1alpha1.IPAllocationStatus{
				Phase:   v1alpha1.IPAllocationPhasePending,
				Address: "",
			},
		},
	}

	used := e.Resolve(cached, "")
	if len(used) != 0 {
		t.Fatalf("expected 0 used IPs for allocation without address, got %d", len(used))
	}
}

func TestExpectations_Resolve_SkipsAllocationsWithoutAllocatedCondition(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	cached := []v1alpha1.IPAllocation{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "alloc-no-condition"},
			Status: v1alpha1.IPAllocationStatus{
				Phase:   v1alpha1.IPAllocationPhaseAllocated,
				Address: "10.0.0.1",
				// No conditions set
			},
		},
	}

	used := e.Resolve(cached, "")
	if len(used) != 0 {
		t.Fatalf("expected 0 used IPs when allocated condition is missing, got %d", len(used))
	}
}

func TestExpectations_Confirm_ClearsStalePendingAllocations(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now}
	e.StageAllocation(alloc)

	// Confirm with an allocation that has the same address at the same time or later
	confirmed := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, now),
	}
	e.Confirm(confirmed)

	// The pending allocation should be cleared, so a different ID can now claim the address
	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: now.Add(1 * time.Second)}
	if !e.StageAllocation(alloc2) {
		t.Fatal("after confirm, the pending allocation should be cleared and address available")
	}
}

func TestExpectations_Confirm_KeepsUnconfirmedPendingAllocations(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: now}
	e.StageAllocation(alloc)

	// Confirm with an empty list — the pending allocation should remain
	e.Confirm(nil)

	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: now}
	if e.StageAllocation(alloc2) {
		t.Fatal("pending allocation should still be blocking after confirm with empty list")
	}
}

func TestExpectations_Confirm_DoesNotClearNewerPendingAllocation(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	olderTime := time.Now()
	newerTime := olderTime.Add(1 * time.Second)

	alloc := IPAllocationPending{ID: "alloc-1", Address: addr, AllocatedAt: newerTime}
	e.StageAllocation(alloc)

	// Confirm with an older allocation time — the pending should not be cleared
	confirmed := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, olderTime),
	}
	e.Confirm(confirmed)

	alloc2 := IPAllocationPending{ID: "alloc-2", Address: addr, AllocatedAt: newerTime.Add(1 * time.Second)}
	if e.StageAllocation(alloc2) {
		t.Fatal("confirm with older alloc time should not clear the newer pending allocation")
	}
}

func TestExpectations_Confirm_ClearsStalePendingReleases(t *testing.T) {
	e := NewIPAllocationExpectations(100 * time.Millisecond) // short timeout for test

	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now().Add(-200 * time.Millisecond) // already past the timeout

	e.StageRelease(addr, releaseTime)

	// Confirm with no allocations — the release should be garbage collected after timeout
	e.Confirm(nil)

	// Verify by staging a cached allocation and resolving: if the release was cleaned up,
	// the address should appear in used IPs
	allocTime := releaseTime.Add(-1 * time.Second)
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, allocTime),
	}
	used := e.Resolve(cached, "")

	// The release was garbage collected, but the allocation is still older than the release
	// time. Since the release was cleaned up from the expectations, it won't be applied
	// during Resolve. The cached allocation should appear.
	if _, ok := used[addr]; !ok {
		t.Fatal("stale pending release should be garbage collected after timeout, making cached allocation visible")
	}
}

func TestExpectations_Confirm_KeepsFreshPendingReleases(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second) // long timeout

	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now()

	e.StageRelease(addr, releaseTime)

	// Confirm with no allocations — the release should NOT be garbage collected (within timeout)
	e.Confirm(nil)

	// The release should still be in effect
	allocTime := releaseTime.Add(-1 * time.Second)
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-1", addr, allocTime),
	}
	used := e.Resolve(cached, "")
	if _, ok := used[addr]; ok {
		t.Fatal("fresh pending release should still suppress the cached allocation")
	}
}

func TestExpectations_Confirm_ClearsOutdatedReleaseWhenNewerAllocationExists(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	releaseTime := time.Now()
	newerAllocTime := releaseTime.Add(1 * time.Second)

	e.StageRelease(addr, releaseTime)

	// Confirm with an allocation newer than the release
	confirmed := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-new", addr, newerAllocTime),
	}
	e.Confirm(confirmed)

	// The release should be cleared, so the address should appear as used
	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-new", addr, newerAllocTime),
	}
	used := e.Resolve(cached, "")
	if _, ok := used[addr]; !ok {
		t.Fatal("release should be cleared when a newer allocation is confirmed")
	}
}

func TestExpectations_Resolve_PendingAllocationOverridesOlderCache(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	olderTime := time.Now()
	newerTime := olderTime.Add(1 * time.Second)

	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-old", addr, olderTime),
	}

	alloc := IPAllocationPending{ID: "alloc-new", Address: addr, AllocatedAt: newerTime}
	e.StageAllocation(alloc)

	used := e.Resolve(cached, "")
	if usedTime, ok := used[addr]; !ok {
		t.Fatal("address should be in used IPs")
	} else if !usedTime.Equal(newerTime) {
		t.Fatalf("expected used time to be the newer pending allocation time, got %v", usedTime)
	}
}

func TestExpectations_Resolve_CacheOverridesOlderPendingAllocation(t *testing.T) {
	e := NewIPAllocationExpectations(5 * time.Second)

	addr := netip.MustParseAddr("10.0.0.1")
	olderTime := time.Now()
	newerTime := olderTime.Add(1 * time.Second)

	cached := []v1alpha1.IPAllocation{
		makeAllocWithAddress("alloc-new", addr, newerTime),
	}

	alloc := IPAllocationPending{ID: "alloc-old", Address: addr, AllocatedAt: olderTime}
	e.StageAllocation(alloc)

	used := e.Resolve(cached, "")
	if usedTime, ok := used[addr]; !ok {
		t.Fatal("address should be in used IPs")
	} else if !usedTime.Equal(newerTime) {
		t.Fatalf("expected used time to be the newer cached time, got %v", usedTime)
	}
}

// makeAllocWithAddress creates an IPAllocation with the given address and an Allocated=True
// condition at the specified time.
func makeAllocWithAddress(name string, addr netip.Addr, allocTime time.Time) v1alpha1.IPAllocation {
	return v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1alpha1.IPAllocationStatus{
			Phase:   v1alpha1.IPAllocationPhaseAllocated,
			Address: addr.String(),
			Conditions: []metav1.Condition{
				{
					Type:               v1alpha1.ConditionTypeAllocated,
					Status:             metav1.ConditionTrue,
					Reason:             "Success",
					LastTransitionTime: metav1.NewTime(allocTime),
				},
			},
		},
	}
}
