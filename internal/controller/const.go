package controller

const (
	FinalizerName = "mikrolb.de/finalizer"
)

const (
	ControllerName = "mikrolb-controller"
)

const (
	AnnotationLBEnabled   = "mikrolb.de/load-balancer-enabled"
	AnnotationLBIPs       = "mikrolb.de/load-balancer-ips"
	AnnotationLBPoolNames = "mikrolb.de/load-balancer-pools"
)

const (
	AnnotationSNATEnabled = "mikrolb.de/snat-enabled"
	AnnotationSNATIPs     = "mikrolb.de/snat-ips"
)

const (
	AnnotationUpdate = "mikrolb.de/update"
)

const (
	LabelServiceName      = "mikrolb.de/service-name"
	LabelServiceNamespace = "mikrolb.de/service-namespace"
	LabelServiceUID       = "mikrolb.de/service-uid"
)
