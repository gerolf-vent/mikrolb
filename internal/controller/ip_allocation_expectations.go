package controller

import (
	"net/netip"
	"sync"
	"time"

	"github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IPAllocationPending struct {
	ID          string
	Address     netip.Addr
	AllocatedAt time.Time
}

// IPAllocationExpectations tracks pending IP allocations and releases to handle eventual consistency in the cache.
type IPAllocationExpectations struct {
	AllocationTimeout  time.Duration
	pendingAllocations map[netip.Addr]IPAllocationPending // address -> pending allocation
	pendingReleases    map[netip.Addr]time.Time           // address -> release time
	mu                 sync.RWMutex
}

func NewIPAllocationExpectations(allocTimeout time.Duration) *IPAllocationExpectations {
	return &IPAllocationExpectations{
		AllocationTimeout:  allocTimeout,
		pendingAllocations: make(map[netip.Addr]IPAllocationPending),
		pendingReleases:    make(map[netip.Addr]time.Time),
	}
}

func (e *IPAllocationExpectations) StageAllocation(alloc IPAllocationPending) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if allocExisting, exists := e.pendingAllocations[alloc.Address]; exists && allocExisting.ID != alloc.ID {
		return false
	}

	e.pendingAllocations[alloc.Address] = alloc
	delete(e.pendingReleases, alloc.Address)

	return true
}

func (e *IPAllocationExpectations) UnstageAllocation(alloc IPAllocationPending) {
	e.mu.Lock()
	defer e.mu.Unlock()

	allocExisting, exists := e.pendingAllocations[alloc.Address]
	if !exists {
		return
	}

	// Only unstage if the existing pending allocation matches the one we want to unstage.
	if allocExisting.ID == alloc.ID && allocExisting.AllocatedAt.Equal(alloc.AllocatedAt) {
		delete(e.pendingAllocations, alloc.Address)
	}
}

func (e *IPAllocationExpectations) StageRelease(ip netip.Addr, releaseTime time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pendingReleases == nil {
		e.pendingReleases = make(map[netip.Addr]time.Time)
	}

	if allocPending, exists := e.pendingAllocations[ip]; exists && allocPending.AllocatedAt.After(releaseTime) {
		// If there is a pending allocation that was created after the release was requested, the
		// release is outdated and should not be staged.
		return
	}

	e.pendingReleases[ip] = releaseTime
	delete(e.pendingAllocations, ip)
}

func (e *IPAllocationExpectations) UnstageRelease(ip netip.Addr, releaseTime time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	existingReleaseTime, exists := e.pendingReleases[ip]
	if !exists {
		return
	}

	// Only unstage if the existing pending release matches the one we want to unstage.
	if existingReleaseTime.Equal(releaseTime) {
		delete(e.pendingReleases, ip)
	}
}

func (e *IPAllocationExpectations) Resolve(fromCache []v1alpha1.IPAllocation, excludeID string) map[netip.Addr]time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := e.buildAllocTimeMap(fromCache)

	// Apply pending allocations
	for ip, allocPending := range e.pendingAllocations {
		if excludeID != "" && allocPending.ID == excludeID {
			continue
		}

		// Only add if there is no existing allocation or if the existing allocation is older
		// than the pending one. This prevents newer allocations from being overwritten by
		// older pending allocations.
		if allocTime, exists := result[ip]; !exists || allocTime.Before(allocPending.AllocatedAt) {
			result[ip] = allocPending.AllocatedAt
		}
	}

	// Apply pending releases
	for ip, releaseTime := range e.pendingReleases {
		// Only delete if the allocation was created before the release time. This prevents
		// deleting allocations that were created after the release was requested.
		if allocTimeExisting, exists := result[ip]; exists && allocTimeExisting.Before(releaseTime) {
			delete(result, ip)
		}
	}

	return result
}

func (e *IPAllocationExpectations) Confirm(allocs []v1alpha1.IPAllocation) {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := e.buildAllocTimeMap(allocs)

	// Remove any pending releases that are not present in the current state anymore
	for ip, releaseTime := range e.pendingReleases {
		allocTime, exists := results[ip]
		if !exists {
			// Only remove the pending release if a safe amount of time has passed since the release
			// was requested.
			if time.Since(releaseTime) > e.AllocationTimeout {
				delete(e.pendingReleases, ip)
			}
		} else if allocTime.After(releaseTime) {
			// If the allocation was created after the release was requested, it means the release is
			// outdated and can be removed
			delete(e.pendingReleases, ip)
		}
	}

	// Remove any pending additions that are already present in the current state
	for ip, allocTime := range results {
		if pendingAlloc, exists := e.pendingAllocations[ip]; exists && !allocTime.Before(pendingAlloc.AllocatedAt) {
			delete(e.pendingAllocations, ip)
		}
	}
}

func (e *IPAllocationExpectations) buildAllocTimeMap(allocs []v1alpha1.IPAllocation) map[netip.Addr]time.Time {
	result := make(map[netip.Addr]time.Time, len(allocs))

	for _, alloc := range allocs {
		addressStr := alloc.Status.Address

		if addressStr == "" {
			continue
		}

		address, err := netip.ParseAddr(addressStr)
		if err != nil {
			continue
		}

		var allocTime time.Time
		condition := meta.FindStatusCondition(alloc.Status.Conditions, v1alpha1.ConditionTypeAllocated)
		if condition != nil && condition.Status == metav1.ConditionTrue {
			allocTime = condition.LastTransitionTime.Time
		} else {
			continue
		}

		if allocTimeExisting, exists := result[address]; !exists || allocTimeExisting.Before(allocTime) {
			result[address] = allocTime
		}
	}

	return result
}
