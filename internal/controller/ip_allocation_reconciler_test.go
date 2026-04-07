package controller

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newIPAllocationReconciler(t *testing.T, k8sClient client.Client, backend *fakeIPAllocationBackend) *IPAllocationReconciler {
	t.Helper()
	return &IPAllocationReconciler{
		client:       k8sClient,
		clientDirect: k8sClient,
		recorder:     getTestRecorder(),
		backend:      backend,
		expectations: NewIPAllocationExpectations(2 * time.Second),
	}
}

func createIPAllocation(t *testing.T, ctx context.Context, k8sClient client.Client, alloc *v1alpha1.IPAllocation) *v1alpha1.IPAllocation {
	t.Helper()
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation %q: %v", alloc.Name, err)
	}
	name := alloc.Name
	t.Cleanup(func() {
		// Strip finalizers and delete with a fresh context so the object actually
		// disappears between tests.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		latest := &v1alpha1.IPAllocation{}
		if err := k8sClient.Get(cleanupCtx, types.NamespacedName{Name: name}, latest); err != nil {
			return
		}
		if len(latest.Finalizers) > 0 {
			latest.Finalizers = nil
			k8sClient.Update(cleanupCtx, latest) // nolint: errcheck
		}
		k8sClient.Delete(cleanupCtx, latest) // nolint: errcheck
	})
	return alloc
}

func reconcileAllocation(t *testing.T, r *IPAllocationReconciler, name string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	return err
}

// reconcileAllocationN reconciles n times, returning the last error.
func reconcileAllocationN(t *testing.T, r *IPAllocationReconciler, name string, n int) error {
	t.Helper()
	var err error
	for i := 0; i < n; i++ {
		err = reconcileAllocation(t, r, name)
	}
	return err
}

func getAllocation(t *testing.T, ctx context.Context, k8sClient client.Client, name string) *v1alpha1.IPAllocation {
	t.Helper()
	got := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		t.Fatalf("failed to fetch allocation %q: %v", name, err)
	}
	return got
}

func TestIPAllocationReconciler_AddsFinalizer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-add-finalizer"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "ether1"}
	r := &IPAllocationReconciler{
		client:       k8sClient,
		clientDirect: k8sClient,
		recorder:     getTestRecorder(),
		backend:      backend,
		expectations: NewIPAllocationExpectations(2 * time.Second),
	}

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatalf("expected finalizer %s to be added", FinalizerName)
	}
}

func TestIPAllocationReconciler_AllocatesAndProgramsAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-pool-program"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:      corev1.IPv4Protocol,
			Addresses:     []string{"10.210.1.10"},
			AutoAssign:    true,
			Advertise:     true,
			AvoidBuggyIPs: true,
			InterfaceName: "vlan10",
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-program-success"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "bridge-v4"}
	r := &IPAllocationReconciler{
		client:       k8sClient,
		clientDirect: k8sClient,
		recorder:     getTestRecorder(),
		backend:      backend,
		expectations: NewIPAllocationExpectations(2 * time.Second),
	}

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}

	if updated.Status.Phase != v1alpha1.IPAllocationPhaseProgrammed {
		t.Fatalf("expected phase %s, got %s", v1alpha1.IPAllocationPhaseProgrammed, updated.Status.Phase)
	}
	if updated.Status.Address != "10.210.1.10" {
		t.Fatalf("expected address 10.210.1.10, got %s", updated.Status.Address)
	}
	if updated.Status.InterfaceName != "bridge-v4" {
		t.Fatalf("expected programmed interface bridge-v4, got %s", updated.Status.InterfaceName)
	}

	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected ready=true condition, got %+v", ready)
	}

	if len(backend.ensureCalls) != 1 || backend.ensureCalls[0].String() != "10.210.1.10" {
		t.Fatalf("expected one ensure advertisement call for 10.210.1.10, got %v", backend.ensureCalls)
	}
}

func TestIPAllocationReconciler_FailsWhenPoolNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-missing-pool"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "pool-does-not-exist"},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	r := &IPAllocationReconciler{
		client:       k8sClient,
		clientDirect: k8sClient,
		recorder:     getTestRecorder(),
		backend:      &fakeIPAllocationBackend{programmedInterface: "ether1"},
		expectations: NewIPAllocationExpectations(2 * time.Second),
	}

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err != nil {
		t.Fatalf("first reconcile should only add finalizer, got: %v", err)
	}

	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err == nil {
		t.Fatalf("expected reconcile error for missing pool")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing pool error, got: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}

	if updated.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
		t.Fatalf("expected phase %s, got %s", v1alpha1.IPAllocationPhaseFailed, updated.Status.Phase)
	}
	allocated := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocated == nil || allocated.Status != metav1.ConditionFalse || allocated.Reason != "PoolNotFound" {
		t.Fatalf("expected allocated condition false with PoolNotFound, got %+v", allocated)
	}
}

func TestIPAllocationReconciler_DeletesAdvertisementOnDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipa-delete-cleanup",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPAllocationSpec{Address: "10.210.9.20"},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}

	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.210.9.20"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	if err := k8sClient.Delete(ctx, alloc); err != nil {
		t.Fatalf("failed to request delete: %v", err)
	}

	backend := &fakeIPAllocationBackend{programmedInterface: "ether1"}
	r := &IPAllocationReconciler{
		client:       k8sClient,
		clientDirect: k8sClient,
		recorder:     getTestRecorder(),
		backend:      backend,
		expectations: NewIPAllocationExpectations(2 * time.Second),
	}

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: alloc.Name}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if len(backend.deleteCalls) != 1 || backend.deleteCalls[0].String() != "10.210.9.20" {
		t.Fatalf("expected one delete advertisement call for 10.210.9.20, got %v", backend.deleteCalls)
	}

	updated := &v1alpha1.IPAllocation{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			t.Fatalf("failed to get allocation after cleanup: %v", err)
		}
		return
	}

	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatalf("expected finalizer to be removed during deletion")
	}
}

// ---------------------------------------------------------------------------
// Specific address allocation
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_AllocatesSpecificAddressInPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-specific-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.211.0.0/24"},
			AutoAssign: false, // explicitly false; specific address allocation must still work
			Advertise:  true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-specific-ok"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.211.0.42"},
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "ether2"}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Address != "10.211.0.42" {
		t.Fatalf("expected address 10.211.0.42, got %q", updated.Status.Address)
	}
	if updated.Status.Phase != v1alpha1.IPAllocationPhaseProgrammed {
		t.Fatalf("expected phase Programmed, got %q", updated.Status.Phase)
	}
	if len(backend.ensureCalls) != 1 || backend.ensureCalls[0].String() != "10.211.0.42" {
		t.Fatalf("expected one ensure call for 10.211.0.42, got %v", backend.ensureCalls)
	}
}

func TestIPAllocationReconciler_FailsWhenAddressInvalid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// We can't easily create an allocation with an invalid address through the API server
	// (CRD validation may not catch this, but the reconciler defends against it). Use a
	// syntactically-invalid address that the reconciler will fail to parse.
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-bad-address"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "not-an-ip"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{})

	// First reconcile adds finalizer.
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	// Second reconcile should fail.
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile error for invalid address")
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
	}
	allocCond := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCond == nil || allocCond.Reason != "AddressInvalid" {
		t.Fatalf("expected Allocated reason AddressInvalid, got %+v", allocCond)
	}
}

func TestIPAllocationReconciler_FailsWhenSpecificAddressNotInAnyPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-other-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.212.0.0/24"},
			Advertise: true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-not-in-pool"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.99.99.5"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{})
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile error for address not in any pool")
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	allocCond := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCond == nil || allocCond.Reason != "AddressNotInPool" {
		t.Fatalf("expected Allocated reason AddressNotInPool, got %+v", allocCond)
	}
}

func TestIPAllocationReconciler_FailsWhenSpecificAddressAlreadyAllocated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-conflict-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.216.0.0/24"},
			Advertise: true,
		},
	})

	// Allocation A claims the address first.
	allocA := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-conflict-a"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.216.0.10"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether3"})
	if err := reconcileAllocationN(t, r, allocA.Name, 2); err != nil {
		t.Fatalf("reconcile A failed: %v", err)
	}
	confirmA := getAllocation(t, ctx, k8sClient, allocA.Name)
	if confirmA.Status.Address != "10.216.0.10" {
		t.Fatalf("expected A to be allocated, got %q", confirmA.Status.Address)
	}

	// Allocation B requests the same address.
	allocB := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-conflict-b"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.216.0.10"},
	})
	if err := reconcileAllocation(t, r, allocB.Name); err != nil {
		t.Fatalf("first reconcile B failed: %v", err)
	}
	if err := reconcileAllocation(t, r, allocB.Name); err == nil {
		t.Fatal("expected reconcile error for duplicate specific address")
	}

	updatedB := getAllocation(t, ctx, k8sClient, allocB.Name)
	allocCondB := apiMeta.FindStatusCondition(updatedB.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCondB == nil || allocCondB.Reason != "AddressAlreadyUsed" {
		t.Fatalf("expected Allocated reason AddressAlreadyUsed, got %+v", allocCondB)
	}
	if updatedB.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
		t.Fatalf("expected B phase Failed, got %q", updatedB.Status.Phase)
	}
}

// ---------------------------------------------------------------------------
// Dynamic allocation
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_DynamicAllocationByPoolName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Pool A has AutoAssign=false; the allocation must still be able to pick from it
	// when the pool is named explicitly.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-named-only"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.220.0.5"},
			AutoAssign: false,
			Advertise:  true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-by-pool-name"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipa-named-only"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether4"})
	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Address != "10.220.0.5" {
		t.Fatalf("expected address 10.220.0.5, got %q", updated.Status.Address)
	}
}

func TestIPAllocationReconciler_DynamicAllocationSkipsNonAutoAssignPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Pool with AutoAssign=false should be skipped when no pool name is given.
	createIPPoolWithFalse(t, ctx, k8sClient, "ipa-skip-pool", corev1.IPv4Protocol,
		[]string{"10.221.0.0/24"}, false /*autoAssign*/, true /*avoidBuggyIPs*/, true /*advertise*/)

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-no-autoassign"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{})
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile error for no auto-assign pools available")
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
		t.Fatalf("expected phase Failed, got %q", updated.Status.Phase)
	}
	allocCond := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCond == nil || allocCond.Reason != "PoolExhausted" {
		t.Fatalf("expected Allocated reason PoolExhausted, got %+v", allocCond)
	}
}

func TestIPAllocationReconciler_DynamicAllocationFiltersByIPFamily(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// IPv6 pool that should be skipped.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-v6-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv6Protocol,
			Addresses:  []string{"fd05::/120"},
			AutoAssign: true,
			Advertise:  true,
		},
	})
	// IPv4 pool that must be picked.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-v4-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.222.0.10"},
			AutoAssign: true,
			Advertise:  true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-v4-only"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether5"})
	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Address != "10.222.0.10" {
		t.Fatalf("expected address 10.222.0.10 from IPv4 pool, got %q", updated.Status.Address)
	}
}

func TestIPAllocationReconciler_AllocatesFromNamedIPv6Pool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-named-v6-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv6Protocol,
			Addresses:  []string{"fd06::/120"},
			AutoAssign: true,
			Advertise:  true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-named-v6-alloc"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipa-named-v6-pool"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether6"})
	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	parsed, err := netip.ParseAddr(updated.Status.Address)
	if err != nil {
		t.Fatalf("expected a valid address, got %q", updated.Status.Address)
	}
	if !parsed.Is6() {
		t.Fatalf("expected IPv6 address, got %s", parsed)
	}
}

func TestIPAllocationReconciler_PoolExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-exhaust-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.223.0.1"}, // single IP
			AutoAssign: true,
			Advertise:  true,
		},
	})

	allocA := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-exhaust-a"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether7"})
	if err := reconcileAllocationN(t, r, allocA.Name, 2); err != nil {
		t.Fatalf("reconcile A failed: %v", err)
	}

	allocB := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-exhaust-b"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})
	if err := reconcileAllocation(t, r, allocB.Name); err != nil {
		t.Fatalf("first reconcile B failed: %v", err)
	}
	if err := reconcileAllocation(t, r, allocB.Name); err == nil {
		t.Fatal("expected reconcile error when pool is exhausted")
	}

	updated := getAllocation(t, ctx, k8sClient, allocB.Name)
	allocCond := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCond == nil || allocCond.Reason != "PoolExhausted" {
		t.Fatalf("expected Allocated reason PoolExhausted, got %+v", allocCond)
	}
}

// ---------------------------------------------------------------------------
// Advertise behavior
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_AllocatesWithoutAdvertising(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPoolWithFalse(t, ctx, k8sClient, "ipa-no-advertise", corev1.IPv4Protocol,
		[]string{"10.224.0.42"}, true /*autoAssign*/, true /*avoidBuggyIPs*/, false /*advertise*/)

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-allocate-only"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "ether8"}
	r := newIPAllocationReconciler(t, k8sClient, backend)
	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	if updated.Status.Phase != v1alpha1.IPAllocationPhaseAllocated {
		t.Fatalf("expected phase Allocated (not Programmed), got %q", updated.Status.Phase)
	}
	if updated.Status.Advertised {
		t.Fatal("expected Advertised=false in status")
	}
	if len(backend.ensureCalls) != 0 {
		t.Fatalf("expected no EnsureIPAdvertisement calls, got %v", backend.ensureCalls)
	}
	// Reconciler proactively cleans up any prior advertisement on the address.
	if len(backend.deleteCalls) == 0 {
		t.Fatal("expected at least one DeleteIPAdvertisement call to proactively clean up")
	}

	programmed := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeProgrammed)
	if programmed == nil || programmed.Status != metav1.ConditionFalse || programmed.Reason != "NotAdvertised" {
		t.Fatalf("expected Programmed=False reason NotAdvertised, got %+v", programmed)
	}

	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != "AllocatedButNotAdvertised" {
		t.Fatalf("expected Ready=True reason AllocatedButNotAdvertised, got %+v", ready)
	}
}

// ---------------------------------------------------------------------------
// Backend programming failure
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_ProgrammingFailureSetsCondition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-program-fail-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.225.0.1"},
			AutoAssign: true,
			Advertise:  true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-program-fail"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})

	backend := &fakeIPAllocationBackend{
		ensureErr: errors.New("router unreachable"),
	}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile to error when programming fails")
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	// Allocation succeeds even though programming fails.
	allocCond := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeAllocated)
	if allocCond == nil || allocCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Allocated=True even when programming fails, got %+v", allocCond)
	}
	programmed := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeProgrammed)
	if programmed == nil || programmed.Status != metav1.ConditionFalse || programmed.Reason != "BackendError" {
		t.Fatalf("expected Programmed=False reason BackendError, got %+v", programmed)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False, got %+v", ready)
	}
}

// ---------------------------------------------------------------------------
// IPv6 allocation
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_IPv6AllocationFromCIDR(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-v6-cidr"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:      corev1.IPv6Protocol,
			Addresses:     []string{"fd07::/120"},
			AutoAssign:    true,
			Advertise:     true,
			AvoidBuggyIPs: true,
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-v6-alloc"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv6Protocol},
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "bridge-v6"}
	r := newIPAllocationReconciler(t, k8sClient, backend)
	if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getAllocation(t, ctx, k8sClient, alloc.Name)
	parsed, err := netip.ParseAddr(updated.Status.Address)
	if err != nil {
		t.Fatalf("expected valid address, got %q", updated.Status.Address)
	}
	if !parsed.Is6() {
		t.Fatalf("expected IPv6 address, got %s", parsed)
	}
	if updated.Status.Phase != v1alpha1.IPAllocationPhaseProgrammed {
		t.Fatalf("expected phase Programmed, got %q", updated.Status.Phase)
	}
}

// ---------------------------------------------------------------------------
// Multiple allocations from same pool
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_MultipleAllocationsFromSamePool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-multi-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.226.0.1-10.226.0.5"},
			AutoAssign: true,
			Advertise:  true,
		},
	})

	backend := &fakeIPAllocationBackend{programmedInterface: "ether9"}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	addresses := map[string]bool{}
	for i := 0; i < 3; i++ {
		name := "ipa-multi-" + string(rune('a'+i))
		alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
		})
		if err := reconcileAllocationN(t, r, alloc.Name, 2); err != nil {
			t.Fatalf("reconcile %s failed: %v", name, err)
		}
		updated := getAllocation(t, ctx, k8sClient, alloc.Name)
		if updated.Status.Address == "" {
			t.Fatalf("allocation %s has no address", name)
		}
		if addresses[updated.Status.Address] {
			t.Fatalf("address %s allocated twice", updated.Status.Address)
		}
		addresses[updated.Status.Address] = true
	}

	if len(addresses) != 3 {
		t.Fatalf("expected 3 distinct addresses, got %d", len(addresses))
	}
}

// ---------------------------------------------------------------------------
// Recovery: failed allocation becomes successful when pool appears
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_RecoversAfterPoolBecomesAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-recover"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipa-recover-pool"},
	})

	r := newIPAllocationReconciler(t, k8sClient, &fakeIPAllocationBackend{programmedInterface: "ether10"})
	// First reconcile adds finalizer.
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("finalizer reconcile failed: %v", err)
	}
	// Second reconcile fails because pool does not exist.
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile to fail before pool exists")
	}
	failed := getAllocation(t, ctx, k8sClient, alloc.Name)
	if failed.Status.Phase != v1alpha1.IPAllocationPhaseFailed {
		t.Fatalf("expected phase Failed, got %q", failed.Status.Phase)
	}

	// Now create the pool.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipa-recover-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:   corev1.IPv4Protocol,
			Addresses:  []string{"10.227.0.1"},
			AutoAssign: true,
			Advertise:  true,
		},
	})

	// Next reconcile should succeed.
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("reconcile after pool creation failed: %v", err)
	}

	recovered := getAllocation(t, ctx, k8sClient, alloc.Name)
	if recovered.Status.Phase != v1alpha1.IPAllocationPhaseProgrammed {
		t.Fatalf("expected phase Programmed after recovery, got %q", recovered.Status.Phase)
	}
	if recovered.Status.Address != "10.227.0.1" {
		t.Fatalf("expected address 10.227.0.1, got %q", recovered.Status.Address)
	}
}

// ---------------------------------------------------------------------------
// Deletion paths
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_DeletionWithoutAddressSkipsBackend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipa-delete-no-address",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	// Move it to Failed (no address) before deletion.
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseFailed
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	if err := k8sClient.Delete(ctx, alloc); err != nil {
		t.Fatalf("failed to delete allocation: %v", err)
	}

	backend := &fakeIPAllocationBackend{}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(backend.deleteCalls) != 0 {
		t.Fatalf("expected no DeleteIPAdvertisement calls, got %v", backend.deleteCalls)
	}

	// Allocation should be gone (finalizer removed).
	got := &v1alpha1.IPAllocation{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, got)
	if err == nil {
		if controllerutil.ContainsFinalizer(got, FinalizerName) {
			t.Fatal("expected finalizer to be removed")
		}
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIPAllocationReconciler_DeletionWithBackendErrorRetains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipa-delete-backend-err",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPAllocationSpec{Address: "10.228.0.5"},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseProgrammed
	alloc.Status.Address = "10.228.0.5"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	if err := k8sClient.Delete(ctx, alloc); err != nil {
		t.Fatalf("failed to delete allocation: %v", err)
	}

	backend := &fakeIPAllocationBackend{deleteErr: errors.New("router unreachable")}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile to fail when backend delete fails")
	}

	// Finalizer must still be present so the controller will retry.
	got := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, got); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if !controllerutil.ContainsFinalizer(got, FinalizerName) {
		t.Fatal("expected finalizer to be retained when backend delete fails")
	}
}

func TestIPAllocationReconciler_DeletionWithoutFinalizerIsNoOp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Create then delete an allocation that never had a finalizer. The reconciler should
	// observe the deletionTimestamp and return without touching the backend.
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipa-delete-no-finalizer",
			Finalizers: []string{"keepalive.example.com"}, // unrelated finalizer keeps object around
		},
		Spec: v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() {
		// Remove the unrelated finalizer so the object can be garbage collected.
		latest := &v1alpha1.IPAllocation{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, latest); err == nil {
			latest.Finalizers = nil
			k8sClient.Update(ctx, latest) // nolint: errcheck
		}
	})

	if err := k8sClient.Delete(ctx, alloc); err != nil {
		t.Fatalf("failed to delete allocation: %v", err)
	}

	backend := &fakeIPAllocationBackend{}
	r := newIPAllocationReconciler(t, k8sClient, backend)
	if err := reconcileAllocation(t, r, alloc.Name); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(backend.deleteCalls) != 0 {
		t.Fatalf("expected no backend calls when our finalizer is not present, got %v", backend.deleteCalls)
	}
}

func TestIPAllocationReconciler_DeletionWithInvalidStatusAddressErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipa-delete-bad-status",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	// Set Allocated phase but with an unparseable address — the reconciler should error
	// during cleanup.
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "not-an-ip"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	if err := k8sClient.Delete(ctx, alloc); err != nil {
		t.Fatalf("failed to delete allocation: %v", err)
	}

	backend := &fakeIPAllocationBackend{}
	r := newIPAllocationReconciler(t, k8sClient, backend)
	if err := reconcileAllocation(t, r, alloc.Name); err == nil {
		t.Fatal("expected reconcile to error when status address is invalid")
	}
	if len(backend.deleteCalls) != 0 {
		t.Fatalf("expected no DeleteIPAdvertisement calls, got %v", backend.deleteCalls)
	}

	// Finalizer must remain so we don't lose track.
	got := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, got); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if !controllerutil.ContainsFinalizer(got, FinalizerName) {
		t.Fatal("expected finalizer to remain after errored cleanup")
	}

	// Manually clear status and finalizer so the cleanup hook can delete the object.
	t.Cleanup(func() {
		latest := &v1alpha1.IPAllocation{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, latest); err == nil {
			latest.Finalizers = nil
			k8sClient.Update(ctx, latest) // nolint: errcheck
		}
	})
}

// ---------------------------------------------------------------------------
// Reconciler returns when allocation is gone
// ---------------------------------------------------------------------------

func TestIPAllocationReconciler_NotFoundIsNoOp(t *testing.T) {
	k8sClient := getTestClient(t)
	backend := &fakeIPAllocationBackend{}
	r := newIPAllocationReconciler(t, k8sClient, backend)

	if err := reconcileAllocation(t, r, "ipa-does-not-exist"); err != nil {
		t.Fatalf("expected no error for missing allocation, got %v", err)
	}
	if len(backend.ensureCalls) != 0 || len(backend.deleteCalls) != 0 {
		t.Fatal("expected no backend calls for missing allocation")
	}
}

type fakeIPAllocationBackend struct {
	ensureCalls []netip.Addr
	deleteCalls []netip.Addr

	programmedInterface string
	ensureErr           error
	deleteErr           error
}

func (f *fakeIPAllocationBackend) Check() (string, error) {
	return "", nil
}

func (f *fakeIPAllocationBackend) Setup() error {
	return nil
}

func (f *fakeIPAllocationBackend) EnsureIPAdvertisement(ip netip.Addr, _ string) (string, error) {
	f.ensureCalls = append(f.ensureCalls, ip)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	if f.programmedInterface == "" {
		return "default-iface", nil
	}
	return f.programmedInterface, nil
}

func (f *fakeIPAllocationBackend) DeleteIPAdvertisement(ip netip.Addr) error {
	f.deleteCalls = append(f.deleteCalls, ip)
	return f.deleteErr
}

func (f *fakeIPAllocationBackend) EnsureService(_ *core.Service) error {
	return nil
}

func (f *fakeIPAllocationBackend) DeleteService(_, _ string) error {
	return nil
}
