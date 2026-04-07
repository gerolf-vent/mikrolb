package controller

import (
	"context"
	"math/big"
	"net/netip"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
	"github.com/gerolf-vent/mikrolb/internal/utils"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func AttachIPPoolController(mgr ctrl.Manager) error {
	r := &IPPoolReconciler{
		client:   mgr.GetClient(),
		recorder: mgr.GetEventRecorder(ControllerName),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.IPPool{},
			// Ignore status updates
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(&v1alpha1.IPAllocation{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
				var pools v1alpha1.IPPoolList
				if err := r.client.List(ctx, &pools); err != nil {
					return nil
				}

				requests := make([]reconcile.Request, len(pools.Items))
				for i, pool := range pools.Items {
					requests[i] = reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(&pool),
					}
				}
				return requests
			}),
		).
		Complete(r)
}

type IPPoolReconciler struct {
	client   client.Client
	recorder events.EventRecorder
}

func (r *IPPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var pool v1alpha1.IPPool
	if err := r.client.Get(ctx, req.NamespacedName, &pool); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get IPPool")
		return ctrl.Result{}, err
	}

	isNew := !controllerutil.ContainsFinalizer(&pool, FinalizerName)
	if isNew {
		poolUpdated := pool.DeepCopy()
		controllerutil.AddFinalizer(poolUpdated, FinalizerName)
		if err := r.client.Patch(ctx, poolUpdated, client.MergeFrom(&pool)); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{}, err
		}
		pool = *poolUpdated
	}

	var allocations v1alpha1.IPAllocationList
	if err := r.client.List(ctx, &allocations); err != nil {
		logger.Error(err, "failed to list IPAllocations")
		return ctrl.Result{}, err
	}

	isDeleting := !pool.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(&pool, FinalizerName)

	// Parse the pool and report any errors as events
	poolAddresses, _ := ParseIPPoolAddresses(pool.Spec.Addresses, core.IPFamily(pool.Spec.IPFamily), pool.Spec.AvoidBuggyIPs)

	totalAddresses := poolAddresses.Count(core.IPFamily(pool.Spec.IPFamily))
	allocatedAddresses := make(map[netip.Addr]struct{})

	for _, alloc := range allocations.Items {
		matchedAddress := false
		var addressAllocated netip.Addr
		if alloc.Status.Address != "" {
			var err error
			addressAllocated, err = netip.ParseAddr(alloc.Status.Address)
			if err != nil {
				logger.Error(err, "failed to parse allocated address", "address", alloc.Status.Address, "allocation", client.ObjectKeyFromObject(&alloc))
			}
		}

		// Check if the allocation matches the pool by address or name
		if addressAllocated.IsValid() && poolAddresses.Contains(addressAllocated) {
			matchedAddress = true
			allocatedAddresses[addressAllocated] = struct{}{}
		}

		matchedPoolName := alloc.Spec.PoolName == pool.Name

		// The allocation has to be updated if either:
		// - it matched the pools addresses but is now being deleted (to trigger deallocation)
		// - it matched the pool name but it's addresses don't match (to trigger reallocation)
		// - the pool is completely new and the allocation matches the pool and has no address yet (to trigger allocation)
		isPendingInPool := alloc.Status.Phase == v1alpha1.IPAllocationPhasePending && (matchedPoolName || alloc.Spec.PoolName == "")
		if (matchedAddress && isDeleting) || (matchedPoolName && !matchedAddress) || (isNew && isPendingInPool) {
			if err := r.triggerIPAllocationUpdate(ctx, &alloc); err != nil {
				logger.Error(err, "failed to trigger allocation update")
				return ctrl.Result{}, err
			}
		}
	}

	if isDeleting {
		poolUpdated := pool.DeepCopy()
		controllerutil.RemoveFinalizer(poolUpdated, FinalizerName)
		if err := r.client.Patch(ctx, poolUpdated, client.MergeFrom(&pool)); err != nil {
			logger.Error(err, "failed to remove finalizer")
			return ctrl.Result{}, err
		}
	} else {
		poolUpdated := pool.DeepCopy()
		poolUpdated.Status.TotalAddresses = utils.FormatCount(totalAddresses)
		poolUpdated.Status.AllocatedAddresses = utils.FormatCount(big.NewInt(0).SetInt64(int64(len(allocatedAddresses))))
		poolUpdated.Status.FreeAddresses = utils.FormatCount(totalAddresses.Sub(totalAddresses, big.NewInt(0).SetInt64(int64(len(allocatedAddresses)))))

		if err := r.client.Status().Patch(ctx, poolUpdated, client.MergeFrom(&pool)); err != nil {
			logger.Error(err, "failed to update IPPool status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *IPPoolReconciler) triggerIPAllocationUpdate(ctx context.Context, alloc *v1alpha1.IPAllocation) error {
	if alloc.Annotations != nil {
		if _, exists := alloc.Annotations[AnnotationUpdate]; exists {
			// Update already triggered
			return nil
		}
	}

	patch := &v1alpha1.IPAllocation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: alloc.APIVersion,
			Kind:       alloc.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      alloc.Name,
			Namespace: alloc.Namespace,
			Annotations: map[string]string{
				AnnotationUpdate: uuid.NewString(),
			},
		},
	}

	return r.client.Patch(ctx, patch,
		client.Apply,
		client.FieldOwner(ControllerName),
		client.ForceOwnership,
	)
}
