package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/core"
)

func AttachServiceController(mgr ctrl.Manager, backend core.Backend, config *core.Config) error {
	r := &ServiceReconciler{
		client:                mgr.GetClient(),
		clientDirect:          mgr.GetAPIReader(),
		recorder:              mgr.GetEventRecorder(ControllerName),
		backend:               backend,
		loadBalancerClassName: config.LoadBalancerClassName,
		isDefaultLoadBalancer: config.IsDefaultLoadBalancer,
		isLBIPModeSupported:   true,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				eps, ok := obj.(*discoveryv1.EndpointSlice)
				if !ok {
					return nil
				}
				serviceName, exists := eps.Labels[discoveryv1.LabelServiceName]
				if !exists || serviceName == "" {
					return nil
				}
				return []reconcile.Request{
					{
						NamespacedName: client.ObjectKey{
							Namespace: eps.Namespace,
							Name:      serviceName,
						},
					},
				}
			},
		)).
		Watches(&v1alpha1.IPAllocation{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				alloc, ok := obj.(*v1alpha1.IPAllocation)
				if !ok {
					return nil
				}

				serviceName, ok := alloc.Labels[LabelServiceName]
				if !ok || serviceName == "" {
					return nil
				}
				serviceNamespace, ok := alloc.Labels[LabelServiceNamespace]
				if !ok || serviceNamespace == "" {
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: client.ObjectKey{
							Namespace: serviceNamespace,
							Name:      serviceName,
						},
					},
				}
			},
		)).
		Complete(r)
}

type ServiceReconciler struct {
	client       client.Client
	clientDirect client.Reader
	recorder     events.EventRecorder

	backend core.Backend

	loadBalancerClassName string
	isDefaultLoadBalancer bool
	isLBIPModeSupported   bool // Since K8s v1.30.0 the IPMode field is available in the LoadBalancerIngress, but we should still be able to run on older versions without it, so we need to check if it's supported before setting it
}

func (c *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var svc corev1.Service
	if err := c.client.Get(ctx, req.NamespacedName, &svc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	isLBConfigured := svc.Spec.Type == corev1.ServiceTypeLoadBalancer && ((svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == c.loadBalancerClassName) || (c.isDefaultLoadBalancer && svc.Spec.LoadBalancerClass == nil))
	isSNATConfigured := false
	if _, ok := svc.Annotations[AnnotationSNATIPs]; ok {
		isSNATConfigured = true
	}

	if (!isLBConfigured && !isSNATConfigured) || !svc.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&svc, FinalizerName) {
			// This service is not configured for load balancing or SNAT (anymore), but
			// it has our finalizer, which means it has state left over, which has
			// to be cleaned up.
			err := c.cleanupService(ctx, req.NamespacedName, string(svc.UID))
			if err != nil {
				return ctrl.Result{}, err
			}

			svcUpdated := svc.DeepCopy()
			controllerutil.RemoveFinalizer(svcUpdated, FinalizerName)
			if err := c.client.Patch(ctx, svcUpdated, client.MergeFrom(&svc)); err != nil {
				return ctrl.Result{}, err
			}

			if svc.DeletionTimestamp.IsZero() {
				svcUpdated.Status.LoadBalancer.Ingress = nil
				if err := c.client.Status().Patch(ctx, svcUpdated, client.MergeFrom(&svc)); err != nil {
					return ctrl.Result{}, err
				}
			}
		}

		return ctrl.Result{}, nil
	}

	// Ensure the finalizer is present. We need this to guarantee that we get a chance to clean up RouterOS
	// resources when the service is deleted.
	if !controllerutil.ContainsFinalizer(&svc, FinalizerName) {
		svcUpdated := svc.DeepCopy()

		controllerutil.AddFinalizer(svcUpdated, FinalizerName)
		err := c.client.Patch(ctx, svcUpdated, client.MergeFrom(&svc))

		// Return here. The patch will trigger a new watch event,
		// which will trigger a new reconcile with the finalizer present.
		return ctrl.Result{}, err
	}

	svc2 := core.Service{
		UID:  string(svc.UID),
		Name: svc.Namespace + "/" + svc.Name,
	}

	allocAutoAssign := true
	var allocLBIPs []netip.Addr
	var allocSNATIPs []netip.Addr
	var allocsExpected []v1alpha1.IPAllocation
	var useLBIPsForSNAT bool

	if isLBConfigured {
		svc2.LBEnabled = true // Default to true if no `mikrolb.de/loadBalancerEnabled` annotation is set

		// Parse `mikrolb.de/load-balancer-enabled` annotation
		if lbEnabled, ok := svc.Annotations[AnnotationLBEnabled]; ok {
			lbEnabledParsed, err := strconv.ParseBool(lbEnabled)
			if err != nil {
				c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Invalid boolean value for annotation %s: %s", AnnotationLBEnabled, lbEnabled)
				svc2.LBEnabled = false // Default to false if the annotation value is invalid
			}
			svc2.LBEnabled = lbEnabledParsed
		}

		// Parse `mikrolb.de/load-balancer-pools` annotation
		if poolNamesStr, ok := svc.Annotations[AnnotationLBPoolNames]; ok && poolNamesStr != "" {
			allocAutoAssign = false
			poolNameList := strings.Split(poolNamesStr, ",")
			for _, poolName := range poolNameList {
				poolName = strings.TrimSpace(poolName)
				poolNameErr := validation.IsDNS1123Subdomain(poolName)
				if len(poolNameErr) > 0 {
					c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Invalid pool name %q in annotation %s: %s", poolName, AnnotationLBPoolNames, strings.Join(poolNameErr, ", "))
				} else {
					allocsExpected = append(allocsExpected, v1alpha1.IPAllocation{
						Spec: v1alpha1.IPAllocationSpec{
							PoolName: poolName,
						},
					})
				}
			}
		}

		// Parse `mikrolb.de/load-balancer-ips` annotation
		if ipsStr, ok := svc.Annotations[AnnotationLBIPs]; ok && ipsStr != "" {
			allocAutoAssign = false
			ipStrList := strings.Split(ipsStr, ",")
			for _, ipStr := range ipStrList {
				ipStr = strings.TrimSpace(ipStr)
				if ipStr == "" {
					continue
				}
				ip, err := netip.ParseAddr(ipStr)
				if err != nil {
					c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Invalid IP address in annotation %s: %s", AnnotationLBIPs, ipStr)
					continue
				}
				allocLBIPs = append(allocLBIPs, ip)
				allocsExpected = append(allocsExpected, v1alpha1.IPAllocation{
					Spec: v1alpha1.IPAllocationSpec{
						Address: ip.String(),
					},
				})
			}
		}

		if allocAutoAssign {
			for _, ipFamily := range svc.Spec.IPFamilies {
				allocsExpected = append(allocsExpected, v1alpha1.IPAllocation{
					Spec: v1alpha1.IPAllocationSpec{
						IPFamily: ipFamily,
					},
				})
			}
		}
	}

	if isSNATConfigured {
		svc2.SNATEnabled = true // Default to true if no `mikrolb.de/snatEnabled` annotation is set

		// Parse `mikrolb.de/snat-enabled` annotation
		if snatEnabled, ok := svc.Annotations[AnnotationSNATEnabled]; ok {
			snatEnabledParsed, err := strconv.ParseBool(snatEnabled)
			if err != nil {
				c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Invalid boolean value for annotation %s: %s", AnnotationSNATEnabled, snatEnabled)
				svc2.SNATEnabled = false // Default to false if the annotation value is invalid
			}
			svc2.SNATEnabled = snatEnabledParsed
		}

		// Parse `mikrolb.de/snat-ips` annotation
		if ipsStr, ok := svc.Annotations[AnnotationSNATIPs]; ok && ipsStr != "" {
			if strings.TrimSpace(ipsStr) == "use-lb-ips" {
				if !isLBConfigured {
					c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Annotation %s cannot be set to 'use-lb-ips' if the service is not configured for load balancing", AnnotationSNATIPs)
				} else {
					useLBIPsForSNAT = true
				}
			} else {
				ipStrList := strings.Split(ipsStr, ",")
				for _, ipStr := range ipStrList {
					ipStr = strings.TrimSpace(ipStr)
					if ipStr == "" {
						continue
					}
					ip, err := netip.ParseAddr(ipStr)
					if err != nil {
						c.recorder.Eventf(&svc, nil, corev1.EventTypeWarning, "AnnotationInvalid", "validation", "Invalid IP address in annotation %s: %s", AnnotationSNATIPs, ipStr)
						continue
					}
					allocSNATIPs = append(allocSNATIPs, ip)
					allocsExpected = append(allocsExpected, v1alpha1.IPAllocation{
						Spec: v1alpha1.IPAllocationSpec{
							Address: ip.String(),
						},
					})
				}
			}
		}
	}

	// Deduplicate expected allocations (because the same IP might be requested both via `mikrolb.de/loadBalancerIPs` and `mikrolb.de/snatIPs`)
	allocsExpected = slices.CompactFunc(allocsExpected, func(a, b v1alpha1.IPAllocation) bool {
		return a.Spec.Address == b.Spec.Address && a.Spec.PoolName == b.Spec.PoolName && a.Spec.IPFamily == b.Spec.IPFamily
	})

	// Generate deterministic unique names for the expected allocations
	for i, a := range allocsExpected {
		specHash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s", svc.UID, a.Spec.Address, a.Spec.PoolName, a.Spec.IPFamily)))
		a.Name = fmt.Sprintf("ip-%x", specHash[:6])
		allocsExpected[i] = a
	}

	var ipal v1alpha1.IPAllocationList
	if err := c.clientDirect.List(ctx, &ipal,
		client.MatchingLabels{
			LabelServiceUID: string(svc.UID),
		},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Delete any unexpected allocations
	for _, alloc := range ipal.Items {
		found := false
		for _, expected := range allocsExpected {
			if alloc.Name == expected.Name {
				found = true
				break
			}
		}
		if !found {
			if err := c.client.Delete(ctx, &alloc); err != nil {
				logger.Error(err, "failed to delete stale IPAllocation", "allocation", client.ObjectKeyFromObject(&alloc))
			}
		}
	}

	// Create any missing expected allocations
	for _, expected := range allocsExpected {
		found := false
		for _, alloc := range ipal.Items {
			if alloc.Name == expected.Name {
				found = true
				break
			}
		}
		if !found {
			expected.Labels = map[string]string{
				LabelServiceUID:       string(svc.UID),
				LabelServiceName:      svc.Name,
				LabelServiceNamespace: svc.Namespace,
			}
			expected.Status.Phase = v1alpha1.IPAllocationPhasePending
			if err := c.client.Create(ctx, &expected); err != nil && !apierrors.IsAlreadyExists(err) {
				logger.Error(err, "failed to create IPAllocation", "allocation", client.ObjectKeyFromObject(&expected))
			}
		}
	}

	svcUpdated := svc.DeepCopy()
	svcUpdated.Status.LoadBalancer.Ingress = nil // Clear LB Ingress in status, we'll update it later with the actual allocated IPs

	// Populate the service with allocated IPs
	for _, alloc := range ipal.Items {
		found := false
		for _, expected := range allocsExpected {
			if alloc.Name == expected.Name {
				found = true
				break
			}
		}

		if !found {
			// Skip allocations that are not expected for this service
			continue
		}

		allocReadyCondition := meta.FindStatusCondition(alloc.Status.Conditions, v1alpha1.ConditionTypeReady)
		if allocReadyCondition == nil || allocReadyCondition.Status != metav1.ConditionTrue {
			// Skip allocations that are not ready yet
			continue
		}

		if alloc.Status.Address == "" {
			// Skip allocations that don't have an address assigned yet
			continue
		}

		ip, err := netip.ParseAddr(alloc.Status.Address)
		if err != nil {
			logger.Error(err, "failed to parse allocated IP address", "address", alloc.Status.Address, "allocation", client.ObjectKeyFromObject(&alloc))
			continue
		}
		isLBIP := slices.Contains(allocLBIPs, ip) || !slices.Contains(allocSNATIPs, ip)
		if isLBIP {
			// Check whether the ip family of the address matches the ones from the service spec
			if ip.Is4() && !slices.Contains(svc.Spec.IPFamilies, corev1.IPv4Protocol) {
				continue
			}
			if ip.Is6() && !slices.Contains(svc.Spec.IPFamilies, corev1.IPv6Protocol) {
				continue
			}

			svc2.LBIPs = append(svc2.LBIPs, ip)

			// Also add the IP to the list of ingresses in the service status
			ingress := corev1.LoadBalancerIngress{
				IP: ip.String(),
			}
			if c.isLBIPModeSupported {
				ipMode := corev1.LoadBalancerIPModeProxy
				ingress.IPMode = &ipMode
			}
			svcUpdated.Status.LoadBalancer.Ingress = append(svcUpdated.Status.LoadBalancer.Ingress, ingress)
		}
		if slices.Contains(allocSNATIPs, ip) || (useLBIPsForSNAT && isLBIP) {
			if ip.Is4() && (!svc2.SNATIPv4.IsValid() || svc2.SNATIPv4.Compare(ip) < 0) {
				svc2.SNATIPv4 = ip
			}
			if ip.Is6() && (!svc2.SNATIPv6.IsValid() || svc2.SNATIPv6.Compare(ip) < 0) {
				svc2.SNATIPv6 = ip
			}
		}
	}

	// Patch the service status in K8s
	if err := c.client.Status().Patch(ctx, svcUpdated, client.MergeFrom(&svc)); err != nil {
		return ctrl.Result{}, err
	}

	var epsl discoveryv1.EndpointSliceList
	if err := c.client.List(ctx, &epsl,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{
			discoveryv1.LabelServiceName: req.Name,
		},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Map the EndpointSlices to our internal representation
	portsByName := make(map[string]int32)
	for _, port := range svc.Spec.Ports {
		portsByName[port.Name] = port.Port
	}

	for _, eps := range epsl.Items {
		var ips []netip.Addr
		for _, ep := range eps.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			if ep.Conditions.Terminating != nil && *ep.Conditions.Terminating {
				continue
			}
			for _, ipStr := range ep.Addresses {
				ip, err := netip.ParseAddr(ipStr)
				if err != nil {
					// Silently skip invalid addresses
					continue
				}
				ips = append(ips, ip)
			}
		}

		var ports []core.EndpointPort
		for _, epPort := range eps.Ports {
			if epPort.Port == nil {
				continue
			}
			var epPortName string
			epPortNumber := *epPort.Port
			epPortProtocol := corev1.ProtocolTCP
			if epPort.Name != nil {
				epPortName = *epPort.Name
			}
			if epPort.Protocol != nil {
				epPortProtocol = *epPort.Protocol
			}
			port, ok := portsByName[epPortName]
			if !ok {
				// Silently skip ports that don't have a matching name in the service spec
				continue
			}
			ports = append(ports, core.EndpointPort{
				Port:       uint16(port),
				TargetPort: uint16(epPortNumber),
				Protocol:   string(epPortProtocol),
			})
		}

		if len(ips) > 0 && len(ports) > 0 {
			svc2.Endpoints = append(svc2.Endpoints, core.Endpoint{
				IPs:   ips,
				Ports: ports,
			})
		}
	}

	// Configure RouterOS with the service
	err := c.backend.EnsureService(&svc2)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *ServiceReconciler) cleanupService(ctx context.Context, namespacedName types.NamespacedName, uid string) error {
	var existingAllocations v1alpha1.IPAllocationList
	labels := client.MatchingLabels{
		LabelServiceUID: string(uid),
	}
	if strings.TrimSpace(uid) == "" {
		labels = client.MatchingLabels{
			LabelServiceNamespace: namespacedName.Namespace,
			LabelServiceName:      namespacedName.Name,
		}
	}
	if err := c.client.List(ctx, &existingAllocations, labels); err != nil {
		return err
	}

	var serviceIPs []netip.Addr
	for _, alloc := range existingAllocations.Items {
		if alloc.Status.Address != "" {
			if ip, err := netip.ParseAddr(alloc.Status.Address); err == nil {
				serviceIPs = append(serviceIPs, ip)
			}
		}
	}

	err := c.backend.DeleteService(namespacedName.String(), uid)
	if err != nil {
		return err
	}

	var failedToFreeIPs []string
	for _, alloc := range existingAllocations.Items {
		if err := c.client.Delete(ctx, &alloc); err != nil {
			failedToFreeIPs = append(failedToFreeIPs, alloc.Status.Address)
		}
	}

	if len(failedToFreeIPs) > 0 {
		return fmt.Errorf("failed to delete ip allocations for ips: %s", strings.Join(failedToFreeIPs, ", "))
	}

	return nil
}
