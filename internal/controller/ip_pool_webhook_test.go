package controller

import (
	"context"
	"strings"
	"testing"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getTestClient(t *testing.T) client.Client {
	c, err := client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}
	return c
}

func TestIPPoolValidator_ValidateCreate_ValidPool(t *testing.T) {
	tests := []struct {
		name    string
		pool    *v1alpha1.IPPool
		wantErr bool
	}{
		{
			name: "valid ipv4 pool with CIDR",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v4-cidr"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/24"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid ipv4 pool with range",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v4-range"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.1-10.0.0.10"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid ipv4 pool with single IP",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v4-single"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.1"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid ipv6 pool with CIDR",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6-cidr"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"fd00::/64"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid pool with exclusion",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-with-exclusion"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/24", "!10.0.0.1"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid pool with multiple addresses",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-multi"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/25", "10.0.1.0/25"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateCreate(context.Background(), tt.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPPoolValidator_ValidateCreate_InvalidAddresses(t *testing.T) {
	tests := []struct {
		name          string
		pool          *v1alpha1.IPPool
		wantErrFields []string
	}{
		{
			name: "invalid ipv4 address",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-invalid-addr"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"invalid-ip"},
				},
			},
			wantErrFields: []string{"spec.addresses[0]"},
		},
		{
			name: "mixed address family with IPv4 pool",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-mixed-family"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"fd00::/64"},
				},
			},
			wantErrFields: []string{"spec.addresses[0]"},
		},
		{
			name: "multiple invalid addresses",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-multi-invalid"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"invalid-1", "10.0.0.0/24", "invalid-2"},
				},
			},
			wantErrFields: []string{"spec.addresses[0]", "spec.addresses[2]"},
		},
		{
			name: "invalid range (reversed)",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-reversed-range"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.10-10.0.0.1"},
				},
			},
			wantErrFields: []string{"spec.addresses[0]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateCreate(context.Background(), tt.pool)
			if err == nil {
				t.Fatalf("ValidateCreate() expected error, got nil")
			}

			statusErr := err.(*apierrors.StatusError)
			if statusErr == nil {
				t.Fatalf("expected apierrors.StatusError, got %T", err)
			}

			fieldErrors := statusErr.ErrStatus.Details.Causes
			if len(fieldErrors) != len(tt.wantErrFields) {
				t.Errorf("got %d field errors, want %d. Errors: %v", len(fieldErrors), len(tt.wantErrFields), fieldErrors)
			}

			for _, wantField := range tt.wantErrFields {
				found := false
				for _, fieldErr := range fieldErrors {
					if fieldErr.Field == wantField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for %q, but not found. Got: %v", wantField, fieldErrors)
				}
			}
		})
	}
}

func TestIPPoolValidator_ValidateCreate_OverlappingPools(t *testing.T) {
	ctx := context.Background()

	pool1 := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-overlap-1"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.0.0.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool1); err != nil {
		t.Fatalf("failed to create first pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool1) // nolint: errcheck
	})

	tests := []struct {
		name    string
		pool    *v1alpha1.IPPool
		wantErr bool
	}{
		{
			name: "pool with complete overlap",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-overlap-2"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/24"},
				},
			},
			wantErr: true,
		},
		{
			name: "pool with partial overlap",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-overlap-3"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.100-10.0.1.100"},
				},
			},
			wantErr: true,
		},
		{
			name: "pool overlapping single IP",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-overlap-4"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.50"},
				},
			},
			wantErr: true,
		},
		{
			name: "pool completely non-overlapping",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-no-overlap"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"192.168.0.0/24"},
				},
			},
			wantErr: false,
		},
		{
			name: "ipv6 pool with ipv4 existing pool (no overlap)",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"fd00::/64"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateCreate(ctx, tt.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.wantErr {
				statusErr := err.(*apierrors.StatusError)
				if statusErr == nil {
					t.Fatalf("expected apierrors.StatusError, got %T", err)
				}
				found := false
				for _, cause := range statusErr.ErrStatus.Details.Causes {
					if cause.Field == "spec.addresses" && strings.Contains(cause.Message, "overlaps with existing pool") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected overlap error message in causes: %v", statusErr.ErrStatus.Details.Causes)
				}
			}
		})
	}
}

func TestIPPoolValidator_ValidateCreate_SelfComparison(t *testing.T) {
	ctx := context.Background()

	pool1 := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.0.0.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool1); err != nil {
		t.Fatalf("failed to create existing pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool1) // nolint: errcheck
	})

	newPool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-pool"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.0.0.0/24"},
		},
	}

	validator := &IPPoolValidator{clientDirect: getTestClient(t)}
	_, err := validator.ValidateCreate(ctx, newPool)
	if err != nil {
		t.Errorf("ValidateCreate() should not error on self-comparison, got: %v", err)
	}
}

func TestIPPoolValidator_ValidateUpdate(t *testing.T) {
	ctx := context.Background()

	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-to-update"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.0.0.0/24"},
		},
	}

	k8sClient := getTestClient(t)
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() {
		k8sClient.Delete(ctx, pool) // nolint: errcheck
	})

	tests := []struct {
		name    string
		oldPool *v1alpha1.IPPool
		newPool *v1alpha1.IPPool
		wantErr bool
	}{
		{
			name: "update to valid non-overlapping addresses",
			oldPool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-to-update"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/24"},
				},
			},
			newPool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-to-update"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"192.168.0.0/24"},
				},
			},
			wantErr: false,
		},
		{
			name: "update to invalid address format",
			oldPool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-to-update"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"10.0.0.0/24"},
				},
			},
			newPool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-to-update"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv4Protocol,
					Addresses: []string{"invalid-address"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateUpdate(ctx, tt.oldPool, tt.newPool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPPoolValidator_ValidateDelete(t *testing.T) {
	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-to-delete"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"10.0.0.0/24"},
		},
	}

	validator := &IPPoolValidator{clientDirect: getTestClient(t)}
	warnings, err := validator.ValidateDelete(context.Background(), pool)
	if err != nil {
		t.Errorf("ValidateDelete() error = %v, want nil", err)
	}
	if warnings != nil && len(warnings) > 0 {
		t.Errorf("ValidateDelete() warnings = %v, want nil", warnings)
	}
}

func TestIPPoolValidator_AvoidBuggyIPs(t *testing.T) {
	tests := []struct {
		name    string
		pool    *v1alpha1.IPPool
		wantErr bool
	}{
		{
			name: "pool with AvoidBuggyIPs enabled",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-avoid-buggy"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:      corev1.IPv4Protocol,
					Addresses:     []string{"10.0.0.0/24"},
					AvoidBuggyIPs: true,
				},
			},
			wantErr: false,
		},
		{
			name: "pool with AvoidBuggyIPs disabled",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-include-buggy"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:      corev1.IPv4Protocol,
					Addresses:     []string{"10.0.0.0/24"},
					AvoidBuggyIPs: false,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateCreate(context.Background(), tt.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPPoolValidator_EdgeCasesIPv6(t *testing.T) {
	tests := []struct {
		name    string
		pool    *v1alpha1.IPPool
		wantErr bool
	}{
		{
			name: "ipv6 full address",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6-full"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"fd37:274a:df59:dead:beef:cafe:babe:1"},
				},
			},
			wantErr: false,
		},
		{
			name: "ipv6 compressed address",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6-compressed"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"::1"},
				},
			},
			wantErr: false,
		},
		{
			name: "ipv6 range",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6-range"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"fd00::1-fd00::10"},
				},
			},
			wantErr: false,
		},
		{
			name: "ipv6 with ipv4 address (mixed family error)",
			pool: &v1alpha1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6-mixed"},
				Spec: v1alpha1.IPPoolSpec{
					IPFamily:  corev1.IPv6Protocol,
					Addresses: []string{"10.0.0.1"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &IPPoolValidator{clientDirect: getTestClient(t)}
			_, err := validator.ValidateCreate(context.Background(), tt.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPPoolValidator_ErrorFormat(t *testing.T) {
	pool := &v1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-invalid"},
		Spec: v1alpha1.IPPoolSpec{
			IPFamily:  corev1.IPv4Protocol,
			Addresses: []string{"invalid-ip"},
		},
	}

	validator := &IPPoolValidator{clientDirect: getTestClient(t)}
	_, err := validator.ValidateCreate(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	statusErr, ok := err.(*apierrors.StatusError)
	if !ok {
		t.Fatalf("expected apierrors.StatusError, got %T: %v", err, err)
	}

	// Check error structure
	if statusErr.ErrStatus.Status != metav1.StatusFailure {
		t.Errorf("Status = %s, want Failure", statusErr.ErrStatus.Status)
	}
	if statusErr.ErrStatus.Reason != metav1.StatusReasonInvalid {
		t.Errorf("Reason = %s, want Invalid", statusErr.ErrStatus.Reason)
	}

	// Check details
	if statusErr.ErrStatus.Details == nil {
		t.Fatal("Details is nil")
	}
	if statusErr.ErrStatus.Details.Kind != "IPPool" {
		t.Errorf("Details.Kind = %s, want IPPool", statusErr.ErrStatus.Details.Kind)
	}
	if len(statusErr.ErrStatus.Details.Causes) == 0 {
		t.Error("expected field errors in Details.Causes")
	}
}
