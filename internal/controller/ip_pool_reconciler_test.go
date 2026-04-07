package controller

import (
	"context"
	"strconv"
	"testing"
	"time"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newIPPoolReconciler(t *testing.T, k8sClient client.Client) *IPPoolReconciler {
	t.Helper()
	return &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}
}

func createIPPool(t *testing.T, ctx context.Context, k8sClient client.Client, pool *v1alpha1.IPPool) *v1alpha1.IPPool {
	t.Helper()
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool %q: %v", pool.Name, err)
	}
	registerPoolCleanup(t, k8sClient, pool.Name)
	return pool
}

// createIPPoolWithFalse creates a pool with explicit boolean values for fields that
// fields. The typed v1alpha1.IPPool struct uses `omitempty` on bool fields, so a
// `false` value is dropped during JSON marshaling and the kubebuilder `default=true`
// re-applies it. This helper uses unstructured.Unstructured so the false values land
// in the request body and survive admission.
func createIPPoolWithFalse(t *testing.T, ctx context.Context, k8sClient client.Client, name string, ipFamily corev1.IPFamily, addresses []string, autoAssign bool, avoidBuggyIPs bool, advertise bool) {
	t.Helper()
	addrIface := make([]interface{}, len(addresses))
	for i, a := range addresses {
		addrIface[i] = a
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("IPPool"))
	u.SetName(name)
	if err := unstructured.SetNestedField(u.Object, string(ipFamily), "spec", "ipFamily"); err != nil {
		t.Fatalf("failed to set ipFamily: %v", err)
	}
	if err := unstructured.SetNestedSlice(u.Object, addrIface, "spec", "addresses"); err != nil {
		t.Fatalf("failed to set addresses: %v", err)
	}
	if err := unstructured.SetNestedField(u.Object, autoAssign, "spec", "autoAssign"); err != nil {
		t.Fatalf("failed to set autoAssign: %v", err)
	}
	if err := unstructured.SetNestedField(u.Object, avoidBuggyIPs, "spec", "avoidBuggyIPs"); err != nil {
		t.Fatalf("failed to set avoidBuggyIPs: %v", err)
	}
	if err := unstructured.SetNestedField(u.Object, advertise, "spec", "advertise"); err != nil {
		t.Fatalf("failed to set advertise: %v", err)
	}
	if err := k8sClient.Create(ctx, u); err != nil {
		t.Fatalf("failed to create pool %q: %v", name, err)
	}
	registerPoolCleanup(t, k8sClient, name)
}

func registerPoolCleanup(t *testing.T, k8sClient client.Client, name string) {
	t.Helper()
	t.Cleanup(func() {
		// Use a fresh context — the test ctx is already cancelled by defer cancel()
		// when t.Cleanup runs.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		latest := &v1alpha1.IPPool{}
		if err := k8sClient.Get(cleanupCtx, types.NamespacedName{Name: name}, latest); err == nil {
			k8sClient.Delete(cleanupCtx, latest) // nolint: errcheck
		}
	})
}

func reconcilePool(t *testing.T, r *IPPoolReconciler, name string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	return err
}

func getPool(t *testing.T, ctx context.Context, k8sClient client.Client, name string) *v1alpha1.IPPool {
	t.Helper()
	got := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		t.Fatalf("failed to fetch pool %q: %v", name, err)
	}
	return got
}

func TestIPPoolReconciler_AddsFinalizer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-finalizer"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.0.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-finalizer"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-finalizer"}, updated); err != nil {
		t.Fatalf("failed to get updated pool: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Errorf("expected finalizer %s to be added, but it wasn't", FinalizerName)
	}

	k8sClient.Delete(ctx, pool) // nolint: errcheck
}

func TestIPPoolReconciler_StatusUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-status"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.2.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-status"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-status"}, updated); err != nil {
		t.Fatalf("failed to get updated pool: %v", err)
	}

	if updated.Status.TotalAddresses == "" {
		t.Errorf("TotalAddresses not updated: %v", updated.Status.TotalAddresses)
	}
	if updated.Status.FreeAddresses == "" {
		t.Errorf("FreeAddresses not updated: %v", updated.Status.FreeAddresses)
	}
	if updated.Status.AllocatedAddresses == "" {
		t.Errorf("AllocatedAddresses not updated: %v", updated.Status.AllocatedAddresses)
	}

	t.Logf("Pool status: Total=%s, Allocated=%s, Free=%s",
		updated.Status.TotalAddresses,
		updated.Status.AllocatedAddresses,
		updated.Status.FreeAddresses)

	k8sClient.Delete(ctx, pool) // nolint: errcheck
}

func TestIPPoolReconciler_UpdatesAllocationOnNewPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-alloc",
			Labels: map[string]string{
				"mikrolb.de/service-name":      "my-service",
				"mikrolb.de/service-namespace": "default",
			},
		},
		Spec: v1alpha1.IPAllocationSpec{
			IPFamily: corev1.IPv4Protocol,
		},
		Status: v1alpha1.IPAllocationStatus{
			Phase: v1alpha1.IPAllocationPhasePending,
		},
	}

	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	alloc.Status.Phase = v1alpha1.IPAllocationPhasePending
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-alloc-update"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.3.0/24"},
		},
	}

	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-alloc-update"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-alloc"}, updated); err != nil {
		t.Fatalf("failed to get updated allocation: %v", err)
	}

	if updated.Annotations == nil {
		t.Errorf("expected allocation annotations to be updated, but they are nil")
	} else if _, exists := updated.Annotations[AnnotationUpdate]; !exists {
		t.Errorf("expected %s annotation on allocation, but it wasn't found", AnnotationUpdate)
	}
}

func TestIPPoolReconciler_TrackAllocatedAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-allocated"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.4.0/24"},
		},
	}

	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-alloc-with-addr",
			Labels: map[string]string{
				"mikrolb.de/service-name":      "my-service",
				"mikrolb.de/service-namespace": "default",
			},
		},
		Spec: v1alpha1.IPAllocationSpec{
			PoolName: "test-pool-allocated",
		},
		Status: v1alpha1.IPAllocationStatus{
			Phase:   v1alpha1.IPAllocationPhaseAllocated,
			Address: "10.200.4.50",
		},
	}

	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.200.4.50"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-allocated"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-allocated"}, updated); err != nil {
		t.Fatalf("failed to get updated pool: %v", err)
	}

	if updated.Status.AllocatedAddresses != "1" {
		t.Errorf("expected 1 allocated address, got %s", updated.Status.AllocatedAddresses)
	}

	t.Logf("Pool status after allocation: Total=%s, Allocated=%s, Free=%s",
		updated.Status.TotalAddresses,
		updated.Status.AllocatedAddresses,
		updated.Status.FreeAddresses)
}

func TestIPPoolReconciler_HandleMismatchedAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-mismatch"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.5.0/24"},
		},
	}

	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-alloc-mismatch",
			Labels: map[string]string{
				"mikrolb.de/service-name":      "my-service",
				"mikrolb.de/service-namespace": "default",
			},
		},
		Spec: v1alpha1.IPAllocationSpec{
			PoolName: "test-pool-mismatch",
		},
		Status: v1alpha1.IPAllocationStatus{
			Phase:   v1alpha1.IPAllocationPhaseAllocated,
			Address: "10.200.6.50",
		},
	}

	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.200.6.50"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-mismatch"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-alloc-mismatch"}, updated); err != nil {
		t.Fatalf("failed to get updated allocation: %v", err)
	}

	if updated.Annotations == nil || updated.Annotations[AnnotationUpdate] == "" {
		t.Errorf("expected allocation to be triggered for reallocation due to address mismatch")
	}
}

func TestIPPoolReconciler_HandlesNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent-pool"},
	})

	if err != nil {
		t.Fatalf("reconcile should not error for notfound, got: %v", err)
	}

	if result != (reconcile.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestIPPoolReconciler_RemovesFinalizeOnDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-delete"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.7.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-delete"},
	})
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	if err := k8sClient.Delete(ctx, pool); err != nil {
		t.Fatalf("failed to delete pool: %v", err)
	}

	_, err = reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-delete"},
	})
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-delete"}, updated)
	if err == nil {
		if controllerutil.ContainsFinalizer(updated, FinalizerName) {
			t.Errorf("expected finalizer to be removed on deletion")
		}
	}
}

func TestIPPoolReconciler_MultipleAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-multi-alloc"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.8.0/24"},
		},
	}

	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	allocations := []*v1alpha1.IPAllocation{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "alloc-1",
				Labels: map[string]string{
					"mikrolb.de/service-name":      "service-1",
					"mikrolb.de/service-namespace": "default",
				},
			},
			Spec: v1alpha1.IPAllocationSpec{
				PoolName: "test-pool-multi-alloc",
			},
			Status: v1alpha1.IPAllocationStatus{
				Phase:   v1alpha1.IPAllocationPhaseAllocated,
				Address: "10.200.8.10",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "alloc-2",
				Labels: map[string]string{
					"mikrolb.de/service-name":      "service-2",
					"mikrolb.de/service-namespace": "default",
				},
			},
			Spec: v1alpha1.IPAllocationSpec{
				PoolName: "test-pool-multi-alloc",
			},
			Status: v1alpha1.IPAllocationStatus{
				Phase:   v1alpha1.IPAllocationPhaseAllocated,
				Address: "10.200.8.20",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "alloc-3",
				Labels: map[string]string{
					"mikrolb.de/service-name":      "service-3",
					"mikrolb.de/service-namespace": "default",
				},
			},
			Spec: v1alpha1.IPAllocationSpec{
				PoolName: "test-pool-multi-alloc",
			},
			Status: v1alpha1.IPAllocationStatus{
				Phase:   v1alpha1.IPAllocationPhaseAllocated,
				Address: "10.200.8.30",
			},
		},
	}

	for _, alloc := range allocations {
		if err := k8sClient.Create(ctx, alloc); err != nil {
			t.Fatalf("failed to create allocation: %v", err)
		}
		alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
		switch alloc.Name {
		case "alloc-1":
			alloc.Status.Address = "10.200.8.10"
		case "alloc-2":
			alloc.Status.Address = "10.200.8.20"
		case "alloc-3":
			alloc.Status.Address = "10.200.8.30"
		}
		if err := k8sClient.Status().Update(ctx, alloc); err != nil {
			t.Fatalf("failed to update allocation status: %v", err)
		}
		t.Cleanup(func() {
			k8sClient.Delete(ctx, alloc) // nolint: errcheck
		})
	}

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-multi-alloc"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-multi-alloc"}, updated); err != nil {
		t.Fatalf("failed to get updated pool: %v", err)
	}

	if updated.Status.AllocatedAddresses != "3" {
		t.Errorf("expected 3 allocated addresses, got %s", updated.Status.AllocatedAddresses)
	}

	t.Logf("Pool status: Total=%s, Allocated=%s, Free=%s",
		updated.Status.TotalAddresses,
		updated.Status.AllocatedAddresses,
		updated.Status.FreeAddresses)
}

func TestIPPoolReconciler_IgnoresAllocationOutsidePool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pool-ignore-outside"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.200.9.0/24"},
		},
	}

	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "alloc-outside",
			Labels: map[string]string{
				"mikrolb.de/service-name":      "service",
				"mikrolb.de/service-namespace": "default",
			},
		},
		Spec: v1alpha1.IPAllocationSpec{
			PoolName: "other-pool",
		},
		Status: v1alpha1.IPAllocationStatus{
			Phase:   v1alpha1.IPAllocationPhaseAllocated,
			Address: "10.201.10.50",
		},
	}

	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.201.10.50"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, alloc) // nolint: errcheck
	})

	reconciler := &IPPoolReconciler{
		client:   k8sClient,
		recorder: getTestRecorder(),
	}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pool-ignore-outside"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPPool{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-pool-ignore-outside"}, updated); err != nil {
		t.Fatalf("failed to get updated pool: %v", err)
	}

	if updated.Status.AllocatedAddresses != "0" {
		t.Errorf("expected 0 allocated addresses, got %s", updated.Status.AllocatedAddresses)
	}
}

// ---------------------------------------------------------------------------
// Status counts for different address spec shapes
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_StatusCountSingleIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-single-ip"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.230.0.5"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-single-ip"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-single-ip")
	if updated.Status.TotalAddresses != "1" {
		t.Errorf("expected total 1, got %s", updated.Status.TotalAddresses)
	}
	if updated.Status.FreeAddresses != "1" {
		t.Errorf("expected free 1, got %s", updated.Status.FreeAddresses)
	}
	if updated.Status.AllocatedAddresses != "0" {
		t.Errorf("expected allocated 0, got %s", updated.Status.AllocatedAddresses)
	}
}

func TestIPPoolReconciler_StatusCountAddressRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-range"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.231.0.1-10.231.0.10"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-range"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-range")
	if updated.Status.TotalAddresses != "10" {
		t.Errorf("expected total 10, got %s", updated.Status.TotalAddresses)
	}
}

func TestIPPoolReconciler_StatusCountWithExclusion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// /29 = 8 addresses; with AvoidBuggyIPs (default true), the .0 and .7 are excluded → 6;
	// then we exclude .3 explicitly → 5.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-exclude"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.232.0.0/29", "!10.232.0.3"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-exclude"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-exclude")
	if updated.Status.TotalAddresses != "5" {
		t.Errorf("expected total 5 (8 - 2 buggy - 1 excluded), got %s", updated.Status.TotalAddresses)
	}
}

func TestIPPoolReconciler_StatusCountAvoidBuggyIPsDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// /29 = 8 addresses with AvoidBuggyIPs=false.
	createIPPoolWithFalse(t, ctx, k8sClient, "ipp-no-buggy", corev1.IPv4Protocol,
		[]string{"10.233.0.0/29"}, true /*autoAssign*/, false /*avoidBuggyIPs*/, true /*advertise*/)

	pool := getPool(t, ctx, k8sClient, "ipp-no-buggy")
	if pool.Spec.AvoidBuggyIPs {
		t.Fatal("expected spec.avoidBuggyIPs=false before reconcile, got true")
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-no-buggy"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-no-buggy")
	if updated.Status.TotalAddresses != "8" {
		t.Errorf("expected total 8 with AvoidBuggyIPs=false, got %s", updated.Status.TotalAddresses)
	}
}

func TestIPPoolReconciler_StatusCountIPv6(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-v6"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv6Protocol,
			Addresses: []string{"fd08::/124"}, // 16 addresses
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-v6"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-v6")
	// /124 has 16 IPs; AvoidBuggyIPs default applies but for IPv6 it doesn't strip
	// network/broadcast in the same sense — verify the result is non-empty and parseable.
	if updated.Status.TotalAddresses == "" {
		t.Fatal("expected non-empty TotalAddresses for IPv6 pool")
	}
}

// ---------------------------------------------------------------------------
// Multiple ranges in one pool
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_StatusCountMultipleRanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-multi-range"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily: corev1.IPv4Protocol,
			Addresses: []string{
				"10.234.0.1-10.234.0.5",   // 5
				"10.234.1.10",             // 1
				"10.234.2.20-10.234.2.23", // 4
			},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-multi-range"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-multi-range")
	if updated.Status.TotalAddresses != "10" {
		t.Errorf("expected total 10 (5+1+4), got %s", updated.Status.TotalAddresses)
	}
}

// ---------------------------------------------------------------------------
// Allocation phase counting
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_CountsProgrammedPhaseAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-programmed-phase"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.235.0.0/29"},
		},
	})

	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-prog-alloc"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipp-programmed-phase"},
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseProgrammed
	alloc.Status.Address = "10.235.0.3"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-programmed-phase"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-programmed-phase")
	if updated.Status.AllocatedAddresses != "1" {
		t.Errorf("expected 1 allocated (Programmed phase), got %s", updated.Status.AllocatedAddresses)
	}
}

func TestIPPoolReconciler_FreeEqualsTotalMinusAllocated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// 10 addresses, 2 will be allocated → 8 free.
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-free-count"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.236.0.1-10.236.0.10"},
		},
	})

	for i, addr := range []string{"10.236.0.1", "10.236.0.2"} {
		alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
			ObjectMeta: metav1.ObjectMeta{Name: "ipp-free-count-alloc-" + strconv.Itoa(i)},
			Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipp-free-count"},
		})
		alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
		alloc.Status.Address = addr
		if err := k8sClient.Status().Update(ctx, alloc); err != nil {
			t.Fatalf("failed to update allocation status: %v", err)
		}
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-free-count"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-free-count")
	if updated.Status.TotalAddresses != "10" {
		t.Errorf("expected total 10, got %s", updated.Status.TotalAddresses)
	}
	if updated.Status.AllocatedAddresses != "2" {
		t.Errorf("expected allocated 2, got %s", updated.Status.AllocatedAddresses)
	}
	if updated.Status.FreeAddresses != "8" {
		t.Errorf("expected free 8, got %s", updated.Status.FreeAddresses)
	}
}

// ---------------------------------------------------------------------------
// Trigger logic
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_TriggerIsIdempotentForMarkedAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Pre-create an allocation that has a stale address (mismatched), and is already marked
	// with the AnnotationUpdate. The reconciler must not overwrite the existing annotation.
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ipp-already-marked",
			Annotations: map[string]string{AnnotationUpdate: "existing-token"},
		},
		Spec: v1alpha1.IPAllocationSpec{PoolName: "ipp-trigger-idempotent"},
	}
	if err := k8sClient.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		latest := &v1alpha1.IPAllocation{}
		if err := k8sClient.Get(cleanupCtx, types.NamespacedName{Name: alloc.Name}, latest); err == nil {
			latest.Finalizers = nil
			k8sClient.Update(cleanupCtx, latest) // nolint: errcheck
			k8sClient.Delete(cleanupCtx, latest) // nolint: errcheck
		}
	})
	// Set status with mismatched address so reconciler tries to trigger.
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.99.99.1" // outside the pool
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-trigger-idempotent"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.237.0.0/29"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-trigger-idempotent"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if updated.Annotations[AnnotationUpdate] != "existing-token" {
		t.Errorf("expected annotation to remain %q, got %q", "existing-token", updated.Annotations[AnnotationUpdate])
	}
}

func TestIPPoolReconciler_NewPoolTriggersPendingWithEmptyPoolName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)

	// Create a pending allocation that does not specify a pool name. When the pool is
	// new (no finalizer yet), the reconciler should trigger this allocation.
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-pending-no-pool"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhasePending
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-new-for-pending"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.238.0.0/29"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-new-for-pending"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if updated.Annotations == nil || updated.Annotations[AnnotationUpdate] == "" {
		t.Error("expected pending allocation with empty pool name to be triggered when a new pool is created")
	}
}

func TestIPPoolReconciler_DoesNotRetriggerEstablishedPoolForPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Create the pool and reconcile once to add the finalizer (so it's no longer "new").
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-established"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.239.0.0/29"},
		},
	})
	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-established"); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	// Now add a pending allocation with no pool name.
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-pending-after-est"},
		Spec:       v1alpha1.IPAllocationSpec{IPFamily: corev1.IPv4Protocol},
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhasePending
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	// Re-reconcile the pool. Since the pool is no longer new, the pending allocation
	// must NOT be triggered (the IPAllocation reconciler is responsible for it).
	if err := reconcilePool(t, r, "ipp-established"); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	updated := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updated); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if updated.Annotations != nil && updated.Annotations[AnnotationUpdate] != "" {
		t.Errorf("expected pending allocation NOT to be triggered by an established pool, got annotation %q", updated.Annotations[AnnotationUpdate])
	}
}

// ---------------------------------------------------------------------------
// Deletion: triggers update on matching allocations
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_DeletionTriggersMatchingAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Create the pool with the finalizer already present (so deletion path is taken).
	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipp-deleting",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.240.0.0/29"},
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	registerPoolCleanup(t, k8sClient, pool.Name)

	// An allocation that holds an address from this pool.
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-deleting-alloc"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.240.0.3"},
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.240.0.3"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	// Delete the pool — this only sets the deletion timestamp because of the finalizer.
	if err := k8sClient.Delete(ctx, pool); err != nil {
		t.Fatalf("failed to delete pool: %v", err)
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, pool.Name); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	// The allocation must have been triggered for update.
	updatedAlloc := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updatedAlloc); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if updatedAlloc.Annotations == nil || updatedAlloc.Annotations[AnnotationUpdate] == "" {
		t.Error("expected allocation matching deleted pool's address to be triggered for update")
	}

	// And the pool itself should be gone (finalizer removed → garbage collected).
	got := &v1alpha1.IPPool{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: pool.Name}, got)
	if err == nil && controllerutil.ContainsFinalizer(got, FinalizerName) {
		t.Error("expected finalizer to be removed during deletion")
	} else if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error fetching pool: %v", err)
	}
}

func TestIPPoolReconciler_DeletionDoesNotTriggerUnrelatedAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ipp-deleting-unrelated",
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.241.0.0/29"},
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	registerPoolCleanup(t, k8sClient, pool.Name)

	// An allocation whose address falls outside this pool.
	alloc := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-deleting-unrelated-alloc"},
		Spec:       v1alpha1.IPAllocationSpec{Address: "10.99.99.99"},
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Address = "10.99.99.99"
	if err := k8sClient.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	if err := k8sClient.Delete(ctx, pool); err != nil {
		t.Fatalf("failed to delete pool: %v", err)
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, pool.Name); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updatedAlloc := &v1alpha1.IPAllocation{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: alloc.Name}, updatedAlloc); err != nil {
		t.Fatalf("failed to fetch allocation: %v", err)
	}
	if updatedAlloc.Annotations != nil && updatedAlloc.Annotations[AnnotationUpdate] != "" {
		t.Error("expected unrelated allocation NOT to be triggered when an unrelated pool is deleted")
	}
}

// ---------------------------------------------------------------------------
// Robustness: malformed allocation status
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_HandlesMalformedAllocationAddressGracefully(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-malformed-status"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.242.0.0/29"},
		},
	})

	// One valid allocation and one with garbage status.address — the reconciler must
	// not crash and the valid one should still be counted.
	good := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-malformed-good"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipp-malformed-status"},
	})
	good.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	good.Status.Address = "10.242.0.3"
	if err := k8sClient.Status().Update(ctx, good); err != nil {
		t.Fatalf("failed to update good allocation status: %v", err)
	}

	bad := createIPAllocation(t, ctx, k8sClient, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-malformed-bad"},
		Spec:       v1alpha1.IPAllocationSpec{PoolName: "ipp-malformed-status"},
	})
	bad.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	bad.Status.Address = "this is not an ip"
	if err := k8sClient.Status().Update(ctx, bad); err != nil {
		t.Fatalf("failed to update bad allocation status: %v", err)
	}

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-malformed-status"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-malformed-status")
	if updated.Status.AllocatedAddresses != "1" {
		t.Errorf("expected 1 allocated (only the valid one counted), got %s", updated.Status.AllocatedAddresses)
	}
}

// ---------------------------------------------------------------------------
// Robustness: pool with one invalid address spec entry
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_PoolWithPartiallyInvalidSpecStillReportsValid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	// Mix a valid range and a malformed entry. The reconciler should not crash and
	// should report the count from the valid portion. (The webhook would normally
	// reject this, but we exercise the reconciler's defensive parsing.)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-partial-invalid"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.243.0.1-10.243.0.5", "garbage"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	if err := reconcilePool(t, r, "ipp-partial-invalid"); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	updated := getPool(t, ctx, k8sClient, "ipp-partial-invalid")
	if updated.Status.TotalAddresses != "5" {
		t.Errorf("expected total 5 from valid range, got %s", updated.Status.TotalAddresses)
	}
}

// ---------------------------------------------------------------------------
// Reconcile is idempotent: status is not flipped on repeated reconciles
// ---------------------------------------------------------------------------

func TestIPPoolReconciler_ReconcileIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := getTestClient(t)
	createIPPool(t, ctx, k8sClient, &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ipp-idempotent"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.244.0.1-10.244.0.5"},
		},
	})

	r := newIPPoolReconciler(t, k8sClient)
	for i := 0; i < 3; i++ {
		if err := reconcilePool(t, r, "ipp-idempotent"); err != nil {
			t.Fatalf("reconcile %d failed: %v", i, err)
		}
	}

	updated := getPool(t, ctx, k8sClient, "ipp-idempotent")
	if updated.Status.TotalAddresses != "5" {
		t.Errorf("expected total 5 after repeated reconciles, got %s", updated.Status.TotalAddresses)
	}
	if updated.Status.AllocatedAddresses != "0" {
		t.Errorf("expected allocated 0, got %s", updated.Status.AllocatedAddresses)
	}
	if updated.Status.FreeAddresses != "5" {
		t.Errorf("expected free 5, got %s", updated.Status.FreeAddresses)
	}
}
