package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
	"github.com/gerolf-vent/mikrolb/internal/routeros/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func AttachIPAllocationController(mgr ctrl.Manager, backend core.Backend, config *core.Config) error {
	r := &IPAllocationReconciler{
		client:       mgr.GetClient(),
		clientDirect: mgr.GetAPIReader(),
		recorder:     mgr.GetEventRecorder(ControllerName),
		backend:      backend,
		expectations: NewIPAllocationExpectations(config.AllocationTimeout),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.IPAllocation{}).
		Complete(r)
}

type IPAllocationReconciler struct {
	client       client.Client
	clientDirect client.Reader
	recorder     events.EventRecorder

	backend core.Backend

	expectations *IPAllocationExpectations
}

func (r *IPAllocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var alloc v1alpha1.IPAllocation
	if err := r.client.Get(ctx, req.NamespacedName, &alloc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get IPAllocation")
		return ctrl.Result{}, err
	}

	if !alloc.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&alloc, FinalizerName) {
			var ip netip.Addr
			if alloc.Status.Phase == v1alpha1.IPAllocationPhaseAllocated || alloc.Status.Phase == v1alpha1.IPAllocationPhaseProgrammed {
				var err error
				ip, err = netip.ParseAddr(alloc.Status.Address)
				if err != nil {
					r.recorder.Eventf(&alloc, nil, corev1.EventTypeWarning, "AddressInvalid", "cleanup", "address %q in status is invalid: %v", alloc.Status.Address, err)
					return ctrl.Result{}, err
				}

				err = r.backend.DeleteIPAdvertisement(ip)
				if err != nil {
					r.recorder.Eventf(&alloc, nil, corev1.EventTypeWarning, "CleanupFailed", "cleanup", "failed to clean up advertisement: %v", err)
					return ctrl.Result{}, err
				}
			}

			releaseTime := time.Now()
			if ip.IsValid() {
				r.expectations.StageRelease(ip, releaseTime)
			}

			allocUpdated := alloc.DeepCopy()
			controllerutil.RemoveFinalizer(allocUpdated, FinalizerName)
			if err := r.client.Patch(ctx, allocUpdated, client.MergeFrom(&alloc)); err != nil {
				r.expectations.UnstageRelease(ip, releaseTime)
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// Ensure the finalizer is present. We need this to guarantee that we get a chance to clean up RouterOS
	// resources when the service is deleted.
	if !controllerutil.ContainsFinalizer(&alloc, FinalizerName) {
		allocUpdated := alloc.DeepCopy()

		controllerutil.AddFinalizer(allocUpdated, FinalizerName)
		err := r.client.Patch(ctx, allocUpdated, client.MergeFrom(&alloc))

		// Return here. The patch will trigger a new watch event,
		// which will trigger a new reconcile with the finalizer present.
		return ctrl.Result{}, err
	}

	var pools v1alpha1.IPPoolList
	// Use direct client to detect IPPool deletions immediately
	if err := r.clientDirect.List(ctx, &pools); err != nil {
		logger.Error(err, "failed to list IPPools")
		return ctrl.Result{}, err
	}

	var allocations v1alpha1.IPAllocationList
	if err := r.client.List(ctx, &allocations); err != nil {
		logger.Error(err, "failed to list IPAllocations")
		return ctrl.Result{}, err
	}

	r.expectations.Confirm(allocations.Items)

	var otherAllocations []v1alpha1.IPAllocation
	for _, a := range allocations.Items {
		if a.UID != alloc.UID {
			otherAllocations = append(otherAllocations, a)
		}
	}

	usedIPs := r.expectations.Resolve(otherAllocations, string(alloc.Name))

	startFullHash := sha256.Sum256([]byte(alloc.Name))
	var startHash [16]byte
	copy(startHash[:], startFullHash[:16])

	var allocatedIP netip.Addr
	var allocatedAdvertised bool
	var allocatedPoolInterfaceName string
	var hasAllocated bool
	var allocReason string
	var allocMsg string
	var allocFail bool
	var allocatedPoolName string
	var stagedAlloc *IPAllocationPending

	if alloc.Spec.Address != "" {
		// Specific IP requested
		reqIP, err := netip.ParseAddr(alloc.Spec.Address)
		if err != nil {
			allocFail = true
			allocReason = "AddressInvalid"
			allocMsg = fmt.Sprintf("Address %q is invalid: %v", alloc.Spec.Address, err)
		} else if _, found := usedIPs[reqIP]; found {
			allocFail = true
			allocReason = "AddressAlreadyUsed"
			allocMsg = fmt.Sprintf("Address %s is already used by another allocation", reqIP.String())
		} else {
			// Find a pool that contains it
			poolFound := false
			for _, p := range pools.Items {
				if alloc.Spec.PoolName != "" && p.Name != alloc.Spec.PoolName {
					continue
				}
				if alloc.Spec.IPFamily != "" && p.Spec.IPFamily != alloc.Spec.IPFamily {
					continue
				}

				// Ignore any errors in the pool spec, because the IPPoolReconciler will report those
				// and invalid ip ranges will not match here anyway
				poolAddresses, _ := ParseIPPoolAddresses(p.Spec.Addresses, core.IPFamily(p.Spec.IPFamily), p.Spec.AvoidBuggyIPs)

				if poolAddresses.Contains(reqIP) {
					poolFound = true
					allocatedPoolName = p.Name
					allocatedAdvertised = p.Spec.Advertise
					allocatedPoolInterfaceName = p.Spec.InterfaceName
					break
				}
			}
			if !poolFound {
				allocFail = true
				allocReason = "AddressNotInPool"
				if alloc.Spec.PoolName != "" {
					allocMsg = fmt.Sprintf("Address %s is not in the specified pool %q", reqIP.String(), alloc.Spec.PoolName)
				} else {
					allocMsg = fmt.Sprintf("Address %s is not in any pool", reqIP.String())
				}
			} else {
				pendingAlloc := IPAllocationPending{
					ID:          string(alloc.Name),
					Address:     reqIP,
					AllocatedAt: time.Now(),
				}
				if r.expectations.StageAllocation(pendingAlloc) {
					allocatedIP = reqIP
					hasAllocated = true
					stagedAlloc = &pendingAlloc
				} else {
					allocFail = true
					allocReason = "AddressAlreadyUsed"
					allocMsg = fmt.Sprintf("Address %s is already used by another allocation", reqIP.String())
				}
			}
		}
	} else {
		// Dynamic allocation
	loop_dynamic_allocation:
		for _, p := range pools.Items {
			if alloc.Spec.PoolName != "" && p.Name != alloc.Spec.PoolName {
				continue
			}
			if alloc.Spec.IPFamily != "" && p.Spec.IPFamily != alloc.Spec.IPFamily {
				continue
			}

			// Ignore any errors in the pool spec, because the IPPoolReconciler will report those
			// and invalid ip ranges will not match here anyway
			poolAddresses, _ := ParseIPPoolAddresses(p.Spec.Addresses, core.IPFamily(p.Spec.IPFamily), p.Spec.AvoidBuggyIPs)

			// If dynamic and no specific pool, must be AutoAssign
			if alloc.Spec.PoolName == "" && !p.Spec.AutoAssign {
				continue
			}

			for _, ipRange := range poolAddresses.RangesForFamily(core.IPFamily(p.Spec.IPFamily)) {
				for ip := range ipRange.Iter(startHash) {
					if _, isIPUsed := usedIPs[ip]; !isIPUsed {
						pendingAlloc := IPAllocationPending{
							ID:          string(alloc.Name),
							Address:     ip,
							AllocatedAt: time.Now(),
						}
						if r.expectations.StageAllocation(pendingAlloc) {
							allocatedIP = ip
							allocatedPoolName = p.Name
							allocatedAdvertised = p.Spec.Advertise
							allocatedPoolInterfaceName = p.Spec.InterfaceName
							hasAllocated = true
							stagedAlloc = &pendingAlloc
							break loop_dynamic_allocation
						}
					}
				}
			}
		}

		if !hasAllocated && !allocFail {
			allocFail = true
			if alloc.Spec.PoolName != "" {
				// Check why it failed
				poolExists := false
				ipFamilyMismatch := false
				for _, p := range pools.Items {
					if p.Name == alloc.Spec.PoolName {
						poolExists = true
						if alloc.Spec.IPFamily != "" && p.Spec.IPFamily != alloc.Spec.IPFamily {
							ipFamilyMismatch = true
						}
						break
					}
				}

				if !poolExists {
					allocReason = "PoolNotFound"
					allocMsg = fmt.Sprintf("Pool %q does not exist", alloc.Spec.PoolName)
				} else if ipFamilyMismatch {
					allocReason = "PoolIPFamilyMismatch"
					allocMsg = fmt.Sprintf("Pool %q has a different IP family", alloc.Spec.PoolName)
				} else {
					allocReason = "PoolExhausted"
					allocMsg = fmt.Sprintf("Pool %q has no free addresses", alloc.Spec.PoolName)
				}
			} else {
				allocReason = "PoolExhausted"
				allocMsg = "No free addresses in any auto-assign pool"
			}
		}
	}

	needsUpdate := false
	var allocatedCondition, programmedCondition, readyCondition metav1.Condition
	programmingFail := false
	readyStatus := metav1.ConditionFalse
	var programmingReason, programmingMsg, readyReason, readyMsg string

	if allocFail {
		r.recorder.Eventf(&alloc, nil, corev1.EventTypeWarning, allocReason, "allocation", allocMsg)

		allocatedCondition = metav1.Condition{
			Type:               v1alpha1.ConditionTypeAllocated,
			Status:             metav1.ConditionFalse,
			Reason:             allocReason,
			Message:            allocMsg,
			ObservedGeneration: alloc.Generation,
		}

		readyReason = allocReason
		readyMsg = allocMsg

		if alloc.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
			alloc.Status.Phase = v1alpha1.IPAllocationPhaseFailed
			alloc.Status.Address = ""
			alloc.Status.Advertised = false
			alloc.Status.InterfaceName = ""
			needsUpdate = true
		}
	} else {
		allocMsg = fmt.Sprintf("Address %s allocated from pool %s", allocatedIP.String(), allocatedPoolName)
		r.recorder.Eventf(&alloc, nil, corev1.EventTypeNormal, "IPAllocated", "allocation", allocMsg)

		allocatedCondition = metav1.Condition{
			Type:    v1alpha1.ConditionTypeAllocated,
			Status:  metav1.ConditionTrue,
			Reason:  "Success",
			Message: allocMsg,
		}

		if alloc.Status.Phase != v1alpha1.IPAllocationPhaseAllocated || alloc.Status.Address != allocatedIP.String() {
			alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
			alloc.Status.Address = allocatedIP.String()
			alloc.Status.Advertised = allocatedAdvertised
			alloc.Status.InterfaceName = allocatedPoolInterfaceName
			needsUpdate = true
		}
	}

	if meta.SetStatusCondition(&alloc.Status.Conditions, allocatedCondition) {
		needsUpdate = true
	}

	if allocatedCondition.Status == metav1.ConditionTrue {
		if allocatedAdvertised {
			interfaceName, err := r.backend.EnsureIPAdvertisement(allocatedIP, allocatedPoolInterfaceName)
			if err != nil {
				programmingReason = "BackendError"
				apiErr, ok := err.(*api.Error)
				if ok {
					programmingMsg = apiErr.Detail
				} else {
					programmingMsg = err.Error()
				}
				programmedCondition = metav1.Condition{
					Type:               v1alpha1.ConditionTypeProgrammed,
					Status:             metav1.ConditionFalse,
					Reason:             programmingReason,
					Message:            strings.ToUpper(programmingMsg[:1]) + programmingMsg[1:], // Capitalize first letter
					ObservedGeneration: alloc.Generation,
				}
				readyReason = programmingReason
				readyMsg = programmingMsg
				programmingFail = true
				r.recorder.Eventf(&alloc, nil, corev1.EventTypeWarning, "ProgrammingFailed", "allocation", "failed to program address on router: %v", err)
			} else {
				programmedCondition = metav1.Condition{
					Type:               v1alpha1.ConditionTypeProgrammed,
					Status:             metav1.ConditionTrue,
					Reason:             "Configured",
					Message:            "Advertised on RouterOS",
					ObservedGeneration: alloc.Generation,
				}
				alloc.Status.Phase = v1alpha1.IPAllocationPhaseProgrammed
				alloc.Status.Advertised = allocatedAdvertised
				alloc.Status.InterfaceName = interfaceName
				needsUpdate = true
				readyReason = "AllocatedAndProgrammed"
				readyMsg = "Address is allocated and advertised"
				readyStatus = metav1.ConditionTrue
			}
		} else {
			err := r.backend.DeleteIPAdvertisement(allocatedIP)
			if err != nil {
				r.recorder.Eventf(&alloc, nil, corev1.EventTypeWarning, "CleanupFailed", "allocation", "failed to clean up advertisement after pool configuration change: %v", err)
			}

			programmedCondition = metav1.Condition{
				Type:               v1alpha1.ConditionTypeProgrammed,
				Status:             metav1.ConditionFalse,
				Reason:             "NotAdvertised",
				Message:            "Address is not advertised according to pool configuration",
				ObservedGeneration: alloc.Generation,
			}
			alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
			needsUpdate = true
			readyReason = "AllocatedButNotAdvertised"
			readyMsg = "Address is allocated but not advertised"
			readyStatus = metav1.ConditionTrue
		}
	} else {
		// If not allocated, it can't be programmed
		programmedCondition = metav1.Condition{
			Type:               v1alpha1.ConditionTypeProgrammed,
			Status:             metav1.ConditionFalse,
			Reason:             "NotAllocated",
			Message:            "Waiting for address allocation",
			ObservedGeneration: alloc.Generation,
		}
	}

	if meta.SetStatusCondition(&alloc.Status.Conditions, programmedCondition) {
		needsUpdate = true
	}

	readyCondition = metav1.Condition{
		Type:               v1alpha1.ConditionTypeReady,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMsg,
		ObservedGeneration: alloc.Generation,
	}

	if meta.SetStatusCondition(&alloc.Status.Conditions, readyCondition) {
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.client.Status().Update(ctx, &alloc); err != nil {
			if stagedAlloc != nil {
				r.expectations.UnstageAllocation(*stagedAlloc)
			}
			logger.Error(err, "failed to update IPAllocation status")
			return ctrl.Result{}, err
		}
	}

	if allocFail {
		return ctrl.Result{}, errors.New("allocation failed: " + allocMsg)
	}
	if programmingFail {
		return ctrl.Result{}, errors.New("programming failed: " + programmingMsg)
	}

	return ctrl.Result{}, nil
}
