package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"testing"
	"time"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func expectedAllocationName(uid string, spec v1alpha1.IPAllocationSpec) string {
	specHash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s", uid, spec.Address, spec.PoolName, spec.IPFamily)))
	return fmt.Sprintf("ip-%x", specHash[:6])
}

func newServiceReconciler(t *testing.T, backend *fakeServiceBackend, opts ...func(*ServiceReconciler)) *ServiceReconciler {
	t.Helper()
	r := &ServiceReconciler{
		client:                getTestClient(t),
		clientDirect:          getTestClient(t),
		backend:               backend,
		recorder:              getTestRecorder(),
		loadBalancerClassName: "mikrolb.de/controller",
		isLBIPModeSupported:   true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func withDefaultLB(r *ServiceReconciler) {
	r.isDefaultLoadBalancer = true
}

func reconcileService(t *testing.T, r *ServiceReconciler, ns, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
}

func reconcileServiceN(t *testing.T, r *ServiceReconciler, ns, name string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		reconcileService(t, r, ns, name)
	}
}

func createService(t *testing.T, ctx context.Context, k client.Client, svc *corev1.Service) *corev1.Service {
	t.Helper()
	if err := k.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	t.Cleanup(func() { k.Delete(ctx, svc) })
	// Re-fetch to get UID
	fetched := &corev1.Service{}
	if err := k.Get(ctx, client.ObjectKeyFromObject(svc), fetched); err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	return fetched
}

func createAllocation(t *testing.T, ctx context.Context, k client.Client, alloc *v1alpha1.IPAllocation) *v1alpha1.IPAllocation {
	t.Helper()
	if err := k.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() { k.Delete(ctx, alloc) })
	return alloc
}

func createReadyAllocation(t *testing.T, ctx context.Context, k client.Client, name, svcUID, svcName, svcNS, address string) *v1alpha1.IPAllocation {
	t.Helper()
	alloc := &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				LabelServiceUID:       svcUID,
				LabelServiceName:      svcName,
				LabelServiceNamespace: svcNS,
			},
		},
		Spec: v1alpha1.IPAllocationSpec{Address: address},
	}
	if err := k.Create(ctx, alloc); err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	t.Cleanup(func() { k.Delete(ctx, alloc) })

	alloc.Status.Address = address
	alloc.Status.Phase = v1alpha1.IPAllocationPhaseAllocated
	alloc.Status.Conditions = []metav1.Condition{{
		Type:               v1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Allocated",
		Message:            "allocation is ready",
		LastTransitionTime: metav1.Now(),
	}}
	if err := k.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}
	return alloc
}

func getService(t *testing.T, ctx context.Context, k client.Client, ns, name string) *corev1.Service {
	t.Helper()
	svc := &corev1.Service{}
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, svc); err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	return svc
}

func listAllocationsForService(t *testing.T, ctx context.Context, k client.Client, svcUID string) []v1alpha1.IPAllocation {
	t.Helper()
	var list v1alpha1.IPAllocationList
	if err := k.List(ctx, &list, client.MatchingLabels{LabelServiceUID: svcUID}); err != nil {
		t.Fatalf("failed to list allocations: %v", err)
	}
	return list.Items
}

func className(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Fake backend
// ---------------------------------------------------------------------------

type fakeServiceBackend struct {
	ensureCalls []*core.Service
	deleteCalls []deleteCall

	ensureErr error
	deleteErr error
}

type deleteCall struct {
	name string
	uid  string
}

func (f *fakeServiceBackend) Check() (string, error)                                  { return "", nil }
func (f *fakeServiceBackend) Setup() error                                             { return nil }
func (f *fakeServiceBackend) EnsureIPAdvertisement(_ netip.Addr, _ string) (string, error) { return "", nil }
func (f *fakeServiceBackend) DeleteIPAdvertisement(_ netip.Addr) error                 { return nil }

func (f *fakeServiceBackend) EnsureService(svc *core.Service) error {
	copySvc := *svc
	copySvc.LBIPs = append([]netip.Addr(nil), svc.LBIPs...)
	copySvc.Endpoints = append([]core.Endpoint(nil), svc.Endpoints...)
	f.ensureCalls = append(f.ensureCalls, &copySvc)
	return f.ensureErr
}

func (f *fakeServiceBackend) DeleteService(name, uid string) error {
	f.deleteCalls = append(f.deleteCalls, deleteCall{name: name, uid: uid})
	return f.deleteErr
}

// ---------------------------------------------------------------------------
// Finalizer
// ---------------------------------------------------------------------------

func TestServiceReconciler_AddsFinalizer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-finalizer", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	if !controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("expected finalizer to be added")
	}
	if len(backend.ensureCalls) != 0 {
		t.Fatal("did not expect backend EnsureService call when only adding finalizer")
	}
}

// ---------------------------------------------------------------------------
// Service selection
// ---------------------------------------------------------------------------

func TestServiceReconciler_IgnoresServiceWithWrongClass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-wrong-class", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("other-lb"),
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, "default", "svc-wrong-class")

	updated := getService(t, ctx, k, "default", "svc-wrong-class")
	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("should not add finalizer to unmanaged service")
	}
}

func TestServiceReconciler_IgnoresClusterIPWithoutSNATAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-clusterip-no-snat", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, "default", "svc-clusterip-no-snat")

	updated := getService(t, ctx, k, "default", "svc-clusterip-no-snat")
	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("should not add finalizer to ClusterIP service without SNAT annotation")
	}
}

func TestServiceReconciler_HandlesDefaultLBClassWhenEnabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-default-class", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:       corev1.ServiceTypeLoadBalancer,
			IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:      []corev1.ServicePort{{Name: "http", Port: 80}},
			// No loadBalancerClass set
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend, withDefaultLB)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, "default", "svc-default-class", 2)

	updated := getService(t, ctx, k, "default", "svc-default-class")
	if !controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("expected finalizer on service handled via default LB class")
	}

	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 1 {
		t.Fatalf("expected 1 auto-assign allocation, got %d", len(allocs))
	}
}

func TestServiceReconciler_IgnoresDefaultLBClassWhenDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-no-default", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:       corev1.ServiceTypeLoadBalancer,
			IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:      []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend) // isDefaultLoadBalancer defaults to false
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, "default", "svc-no-default")

	updated := getService(t, ctx, k, "default", "svc-no-default")
	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("should not add finalizer when default LB is disabled and no class is set")
	}
}

func TestServiceReconciler_HandlesSNATOnlyService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-snat-only",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationSNATIPs: "10.0.0.50",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	if !controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("expected finalizer for SNAT-only service")
	}

	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 1 {
		t.Fatalf("expected 1 SNAT allocation, got %d", len(allocs))
	}
	if allocs[0].Spec.Address != "10.0.0.50" {
		t.Fatalf("expected allocation address 10.0.0.50, got %s", allocs[0].Spec.Address)
	}
}

func TestServiceReconciler_NotFoundReturnsEmpty(t *testing.T) {
	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	reconcileService(t, r, "default", "svc-does-not-exist")
	if len(backend.ensureCalls) != 0 || len(backend.deleteCalls) != 0 {
		t.Fatal("expected no backend calls for missing service")
	}
}

// ---------------------------------------------------------------------------
// Auto-assign allocations
// ---------------------------------------------------------------------------

func TestServiceReconciler_CreatesAutoAssignIPAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-auto-alloc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 1 {
		t.Fatalf("expected 1 allocation, got %d", len(allocs))
	}
	if allocs[0].Spec.IPFamily != corev1.IPv4Protocol {
		t.Fatalf("expected IPv4 allocation, got %s", allocs[0].Spec.IPFamily)
	}
	if allocs[0].Labels[LabelServiceName] != svc.Name {
		t.Fatalf("expected service-name label %q, got %q", svc.Name, allocs[0].Labels[LabelServiceName])
	}
	if allocs[0].Labels[LabelServiceNamespace] != svc.Namespace {
		t.Fatalf("expected service-namespace label %q, got %q", svc.Namespace, allocs[0].Labels[LabelServiceNamespace])
	}
}

func TestServiceReconciler_DualStackAutoAssignCreatesTwoAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	dualStackPolicy := corev1.IPFamilyPolicyRequireDualStack
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-dualstack", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			IPFamilyPolicy:    &dualStackPolicy,
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations for dual-stack, got %d", len(allocs))
	}

	families := map[corev1.IPFamily]bool{}
	for _, a := range allocs {
		families[a.Spec.IPFamily] = true
	}
	if !families[corev1.IPv4Protocol] || !families[corev1.IPv6Protocol] {
		t.Fatalf("expected one IPv4 and one IPv6 allocation, got families=%v", families)
	}
}

// ---------------------------------------------------------------------------
// Explicit IP and pool annotations
// ---------------------------------------------------------------------------

func TestServiceReconciler_ExplicitIPAnnotationCreatesAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-explicit-ips",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1,10.0.0.2",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations for 2 explicit IPs, got %d", len(allocs))
	}

	addresses := map[string]bool{}
	for _, a := range allocs {
		addresses[a.Spec.Address] = true
	}
	if !addresses["10.0.0.1"] || !addresses["10.0.0.2"] {
		t.Fatalf("expected allocations for 10.0.0.1 and 10.0.0.2, got %v", addresses)
	}
}

func TestServiceReconciler_PoolAnnotationCreatesAllocations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-pool-alloc",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBPoolNames: "pool-a,pool-b",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations for 2 pool names, got %d", len(allocs))
	}

	pools := map[string]bool{}
	for _, a := range allocs {
		pools[a.Spec.PoolName] = true
	}
	if !pools["pool-a"] || !pools["pool-b"] {
		t.Fatalf("expected allocations for pool-a and pool-b, got %v", pools)
	}
}

// ---------------------------------------------------------------------------
// SNAT annotations
// ---------------------------------------------------------------------------

func TestServiceReconciler_SNATUseLBIPs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-snat-use-lb",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs:   "10.0.0.1",
				AnnotationSNATIPs: "use-lb-ips",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}
	ensured := backend.ensureCalls[0]
	if !ensured.SNATIPv4.IsValid() || ensured.SNATIPv4.String() != "10.0.0.1" {
		t.Fatalf("expected SNATIPv4 10.0.0.1, got %v", ensured.SNATIPv4)
	}
}

func TestServiceReconciler_SNATExplicitIPs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-snat-explicit",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs:   "10.0.0.1",
				AnnotationSNATIPs: "10.0.0.99",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)

	// Create allocations for both the LB IP and the SNAT IP
	lbSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), lbSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)
	snatSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.99"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), snatSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.99",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}
	ensured := backend.ensureCalls[0]
	if !ensured.SNATIPv4.IsValid() || ensured.SNATIPv4.String() != "10.0.0.99" {
		t.Fatalf("expected SNATIPv4 10.0.0.99, got %v", ensured.SNATIPv4)
	}
}

// ---------------------------------------------------------------------------
// LB/SNAT enabled annotations
// ---------------------------------------------------------------------------

func TestServiceReconciler_LBDisabledStillAllocatesButDoesNotEnableLB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-lb-disabled",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBEnabled: "false",
				AnnotationLBIPs:    "10.0.0.1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}
	if backend.ensureCalls[0].LBEnabled {
		t.Fatal("expected LBEnabled to be false")
	}
}

func TestServiceReconciler_SNATDisabledStillAllocatesButDoesNotEnableSNAT(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-snat-disabled",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationSNATIPs:     "10.0.0.50",
				AnnotationSNATEnabled: "false",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.50"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.50",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}
	if backend.ensureCalls[0].SNATEnabled {
		t.Fatal("expected SNATEnabled to be false")
	}
}

// ---------------------------------------------------------------------------
// Allocation deduplication
// ---------------------------------------------------------------------------

func TestServiceReconciler_DeduplicatesSameIPInLBAndSNAT(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-dedup",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs:   "10.0.0.1",
				AnnotationSNATIPs: "10.0.0.1", // same IP as LB
			},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileServiceN(t, r, svc.Namespace, svc.Name, 2)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 1 {
		t.Fatalf("expected 1 deduplicated allocation, got %d", len(allocs))
	}
}

// ---------------------------------------------------------------------------
// Stale allocation cleanup
// ---------------------------------------------------------------------------

func TestServiceReconciler_DeletesStaleAllocationsOnAnnotationChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-stale-cleanup",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.2",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)

	// Create a stale allocation for the OLD IP that is no longer desired
	oldAllocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	staleAlloc := createAllocation(t, ctx, k, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: expectedAllocationName(string(updated.UID), oldAllocSpec),
			Labels: map[string]string{
				LabelServiceUID:       string(updated.UID),
				LabelServiceName:      svc.Name,
				LabelServiceNamespace: svc.Namespace,
			},
		},
		Spec: oldAllocSpec,
	})

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	// The stale allocation should be deleted
	err := k.Get(ctx, types.NamespacedName{Name: staleAlloc.Name}, &v1alpha1.IPAllocation{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale allocation to be deleted, got err=%v", err)
	}

	// The new allocation should be created
	allocs := listAllocationsForService(t, ctx, k, string(updated.UID))
	if len(allocs) != 1 {
		t.Fatalf("expected 1 allocation after cleanup, got %d", len(allocs))
	}
	if allocs[0].Spec.Address != "10.0.0.2" {
		t.Fatalf("expected allocation for 10.0.0.2, got %s", allocs[0].Spec.Address)
	}
}

// ---------------------------------------------------------------------------
// Ingress population and IPMode
// ---------------------------------------------------------------------------

func TestServiceReconciler_PopulatesIngressFromReadyAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.203.0.10",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.203.0.10"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.203.0.10",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	result := getService(t, ctx, k, svc.Namespace, svc.Name)
	if len(result.Status.LoadBalancer.Ingress) != 1 {
		t.Fatalf("expected 1 ingress, got %d", len(result.Status.LoadBalancer.Ingress))
	}
	ingress := result.Status.LoadBalancer.Ingress[0]
	if ingress.IP != "10.203.0.10" {
		t.Fatalf("expected ingress IP 10.203.0.10, got %s", ingress.IP)
	}
	if ingress.IPMode == nil {
		t.Fatal("expected IPMode to be set")
	}
	if *ingress.IPMode != corev1.LoadBalancerIPModeProxy {
		t.Fatalf("expected IPMode Proxy, got %v", *ingress.IPMode)
	}
}

func TestServiceReconciler_SkipsAllocationNotReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-alloc-not-ready",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)

	// Create a pending (not ready) allocation
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	alloc := createAllocation(t, ctx, k, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: expectedAllocationName(string(updated.UID), allocSpec),
			Labels: map[string]string{
				LabelServiceUID:       string(updated.UID),
				LabelServiceName:      svc.Name,
				LabelServiceNamespace: svc.Namespace,
			},
		},
		Spec: allocSpec,
	})
	alloc.Status.Phase = v1alpha1.IPAllocationPhasePending
	alloc.Status.Address = "10.0.0.1"
	// No Ready condition
	if err := k.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	result := getService(t, ctx, k, svc.Namespace, svc.Name)
	if len(result.Status.LoadBalancer.Ingress) != 0 {
		t.Fatalf("expected no ingress for not-ready allocation, got %d", len(result.Status.LoadBalancer.Ingress))
	}
}

func TestServiceReconciler_FiltersIngressByIPFamily(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-family-filter",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1,fd00::1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol}, // Only IPv4
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)

	// Create ready allocations for both IPv4 and IPv6
	v4Spec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), v4Spec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)
	v6Spec := v1alpha1.IPAllocationSpec{Address: "fd00::1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), v6Spec),
		string(updated.UID), svc.Name, svc.Namespace, "fd00::1",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	result := getService(t, ctx, k, svc.Namespace, svc.Name)
	if len(result.Status.LoadBalancer.Ingress) != 1 {
		t.Fatalf("expected 1 ingress (IPv4 only), got %d", len(result.Status.LoadBalancer.Ingress))
	}
	if result.Status.LoadBalancer.Ingress[0].IP != "10.0.0.1" {
		t.Fatalf("expected ingress IP 10.0.0.1, got %s", result.Status.LoadBalancer.Ingress[0].IP)
	}
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestServiceReconciler_CleanupWhenServiceNoLongerManaged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "svc-cleanup",
			Namespace:  "default",
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("other-lb-class"),
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	current := getService(t, ctx, k, svc.Namespace, svc.Name)
	current.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "10.202.0.10"}}
	if err := k.Status().Update(ctx, current); err != nil {
		t.Fatalf("failed to set initial status: %v", err)
	}

	alloc := createAllocation(t, ctx, k, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-cleanup-alloc",
			Labels: map[string]string{
				LabelServiceUID:       string(current.UID),
				LabelServiceName:      current.Name,
				LabelServiceNamespace: current.Namespace,
			},
		},
		Spec: v1alpha1.IPAllocationSpec{Address: "10.202.0.10"},
	})
	alloc.Status.Address = "10.202.0.10"
	if err := k.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("expected finalizer to be removed")
	}
	if len(updated.Status.LoadBalancer.Ingress) != 0 {
		t.Fatalf("expected ingress to be cleared, got %v", updated.Status.LoadBalancer.Ingress)
	}
	if len(backend.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeleteService call, got %d", len(backend.deleteCalls))
	}
	if backend.deleteCalls[0].name != "default/svc-cleanup" {
		t.Fatalf("unexpected DeleteService name: %q", backend.deleteCalls[0].name)
	}
	if backend.deleteCalls[0].uid != string(current.UID) {
		t.Fatalf("unexpected DeleteService UID: %q", backend.deleteCalls[0].uid)
	}

	err := k.Get(ctx, types.NamespacedName{Name: "svc-cleanup-alloc"}, &v1alpha1.IPAllocation{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected allocation to be deleted, got err=%v", err)
	}
}

func TestServiceReconciler_CleanupOnServiceDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "svc-deleted",
			Namespace:  "default",
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	}
	if err := k.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	current := getService(t, ctx, k, svc.Namespace, svc.Name)

	alloc := createAllocation(t, ctx, k, &v1alpha1.IPAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-deleted-alloc",
			Labels: map[string]string{
				LabelServiceUID:       string(current.UID),
				LabelServiceName:      current.Name,
				LabelServiceNamespace: current.Namespace,
			},
		},
		Spec: v1alpha1.IPAllocationSpec{Address: "10.0.0.1"},
	})
	alloc.Status.Address = "10.0.0.1"
	if err := k.Status().Update(ctx, alloc); err != nil {
		t.Fatalf("failed to update allocation status: %v", err)
	}

	// Delete the service (sets DeletionTimestamp, finalizer blocks actual removal)
	if err := k.Delete(ctx, svc); err != nil {
		t.Fatalf("failed to delete service: %v", err)
	}

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeleteService call, got %d", len(backend.deleteCalls))
	}

	// Finalizer should be removed, allowing actual deletion
	updated := &corev1.Service{}
	err := k.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, updated)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected error: %v", err)
		}
		// Already fully deleted — expected
		return
	}
	if controllerutil.ContainsFinalizer(updated, FinalizerName) {
		t.Fatal("expected finalizer to be removed after deletion")
	}
}

// ---------------------------------------------------------------------------
// Backend integration: endpoints
// ---------------------------------------------------------------------------

func TestServiceReconciler_MapsEndpointSlicesToBackend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-eps",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)

	// Create ready allocation
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)

	// Create an EndpointSlice
	ready := true
	epPort := int32(8080)
	epPortName := "http"
	protocol := corev1.ProtocolTCP
	eps := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-eps-slice",
			Namespace: "default",
			Labels: map[string]string{
				discoveryv1.LabelServiceName: "svc-eps",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"172.16.0.10", "172.16.0.11"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: &epPortName, Port: &epPort, Protocol: &protocol},
		},
	}
	if err := k.Create(ctx, eps); err != nil {
		t.Fatalf("failed to create EndpointSlice: %v", err)
	}
	t.Cleanup(func() { k.Delete(ctx, eps) })

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}

	ensured := backend.ensureCalls[0]
	if len(ensured.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint group, got %d", len(ensured.Endpoints))
	}

	ep := ensured.Endpoints[0]
	if len(ep.IPs) != 2 {
		t.Fatalf("expected 2 endpoint IPs, got %d", len(ep.IPs))
	}
	if ep.IPs[0].String() != "172.16.0.10" || ep.IPs[1].String() != "172.16.0.11" {
		t.Fatalf("unexpected endpoint IPs: %v", ep.IPs)
	}
	if len(ep.Ports) != 1 {
		t.Fatalf("expected 1 endpoint port, got %d", len(ep.Ports))
	}
	if ep.Ports[0].Port != 80 {
		t.Fatalf("expected service port 80, got %d", ep.Ports[0].Port)
	}
	if ep.Ports[0].TargetPort != 8080 {
		t.Fatalf("expected target port 8080, got %d", ep.Ports[0].TargetPort)
	}
	if ep.Ports[0].Protocol != "TCP" {
		t.Fatalf("expected protocol TCP, got %s", ep.Ports[0].Protocol)
	}
}

func TestServiceReconciler_SkipsNotReadyEndpoints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-eps-notready",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)

	ready := true
	notReady := false
	terminating := true
	epPort := int32(8080)
	epPortName := "http"
	protocol := corev1.ProtocolTCP
	eps := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-eps-notready-slice",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "svc-eps-notready"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"172.16.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			{Addresses: []string{"172.16.0.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
			{Addresses: []string{"172.16.0.12"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready, Terminating: &terminating}},
		},
		Ports: []discoveryv1.EndpointPort{{Name: &epPortName, Port: &epPort, Protocol: &protocol}},
	}
	if err := k.Create(ctx, eps); err != nil {
		t.Fatalf("failed to create EndpointSlice: %v", err)
	}
	t.Cleanup(func() { k.Delete(ctx, eps) })

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}

	ensured := backend.ensureCalls[0]
	if len(ensured.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint group, got %d", len(ensured.Endpoints))
	}
	if len(ensured.Endpoints[0].IPs) != 1 {
		t.Fatalf("expected 1 ready endpoint IP, got %d", len(ensured.Endpoints[0].IPs))
	}
	if ensured.Endpoints[0].IPs[0].String() != "172.16.0.10" {
		t.Fatalf("expected only the ready endpoint 172.16.0.10, got %v", ensured.Endpoints[0].IPs)
	}
}

// ---------------------------------------------------------------------------
// Backend service model
// ---------------------------------------------------------------------------

func TestServiceReconciler_BackendReceivesCorrectServiceModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k := getTestClient(t)
	svc := createService(t, ctx, k, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-model",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationLBIPs: "10.0.0.1",
			},
			Finalizers: []string{FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: className("mikrolb.de/controller"),
			IPFamilies:        []corev1.IPFamily{corev1.IPv4Protocol},
			Ports:             []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	})

	updated := getService(t, ctx, k, svc.Namespace, svc.Name)
	allocSpec := v1alpha1.IPAllocationSpec{Address: "10.0.0.1"}
	createReadyAllocation(t, ctx, k,
		expectedAllocationName(string(updated.UID), allocSpec),
		string(updated.UID), svc.Name, svc.Namespace, "10.0.0.1",
	)

	backend := &fakeServiceBackend{}
	r := newServiceReconciler(t, backend)
	r.client = k
	r.clientDirect = k
	reconcileService(t, r, svc.Namespace, svc.Name)

	if len(backend.ensureCalls) != 1 {
		t.Fatalf("expected 1 EnsureService call, got %d", len(backend.ensureCalls))
	}

	ensured := backend.ensureCalls[0]
	if ensured.UID != string(updated.UID) {
		t.Fatalf("expected UID %s, got %s", updated.UID, ensured.UID)
	}
	if ensured.Name != "default/svc-model" {
		t.Fatalf("expected name default/svc-model, got %s", ensured.Name)
	}
	if !ensured.LBEnabled {
		t.Fatal("expected LBEnabled to be true")
	}
	if len(ensured.LBIPs) != 1 || ensured.LBIPs[0].String() != "10.0.0.1" {
		t.Fatalf("expected LBIPs [10.0.0.1], got %v", ensured.LBIPs)
	}
}
