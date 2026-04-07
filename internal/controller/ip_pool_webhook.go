package controller

import (
	"context"
	"fmt"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func AttachIPPoolWebhook(mgr ctrl.Manager) error {
	v := &IPPoolValidator{
		clientDirect: mgr.GetAPIReader(),
	}

	return ctrl.NewWebhookManagedBy(mgr, &v1alpha1.IPPool{}).
		WithValidator(v).
		Complete()
}

type IPPoolValidator struct {
	clientDirect client.Reader
}

func (v *IPPoolValidator) ValidateCreate(ctx context.Context, pool *v1alpha1.IPPool) (admission.Warnings, error) {
	return nil, v.validate(ctx, pool)
}

func (v *IPPoolValidator) ValidateUpdate(ctx context.Context, _, poolNew *v1alpha1.IPPool) (admission.Warnings, error) {
	return nil, v.validate(ctx, poolNew)
}

func (v *IPPoolValidator) ValidateDelete(_ context.Context, _ *v1alpha1.IPPool) (admission.Warnings, error) {
	return nil, nil
}

func (v *IPPoolValidator) validate(ctx context.Context, pool *v1alpha1.IPPool) error {
	var pools v1alpha1.IPPoolList
	if err := v.clientDirect.List(ctx, &pools); err != nil {
		return apierrors.NewInternalError(fmt.Errorf("unable to list existing IPPools: %w", err))
	}

	addressPath := field.NewPath("spec", "addresses")
	allErrs := field.ErrorList{}

	poolAddresses, errs := ParseIPPoolAddresses(pool.Spec.Addresses, core.IPFamily(pool.Spec.IPFamily), pool.Spec.AvoidBuggyIPs)
	for i, err := range errs {
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				addressPath.Index(i),
				pool.Spec.Addresses[i],
				err.Error(),
			))
		}
	}

	for _, poolExisting := range pools.Items {
		if poolExisting.Name == pool.Name {
			continue
		}

		poolExistingAddressesParsed, _ := ParseIPPoolAddresses(poolExisting.Spec.Addresses, core.IPFamily(poolExisting.Spec.IPFamily), poolExisting.Spec.AvoidBuggyIPs)

		intersections := poolAddresses.Intersections(poolExistingAddressesParsed)
		if len(intersections.Ranges()) > 0 {
			allErrs = append(allErrs, field.Invalid(
				addressPath,
				pool.Spec.Addresses,
				fmt.Sprintf("overlaps with existing pool %q in address(es): %s", poolExisting.Name, intersections.String()),
			))
		}
	}

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(v1alpha1.GroupVersion.WithKind("IPPool").GroupKind(), pool.Name, allErrs)
	}

	return nil
}
