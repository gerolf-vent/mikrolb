package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPAllocation tracks the allocation of a single IP address for a service. The service
// reference is stored in the labels `mikrolb.de/service-namespace` and `mikrolb.de/service-name`.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ipa
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.metadata.labels.mikrolb\.de/service-namespace`
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.metadata.labels.mikrolb\.de/service-name`
// +kubebuilder:printcolumn:name="Family",type=string,JSONPath=`.spec.ipFamily`,priority=1
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.spec.poolName`,priority=1
// +kubebuilder:printcolumn:name="Configured Address",type=string,JSONPath=`.spec.address`,priority=1
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.status.address`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type IPAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Desired state of the IP allocation.
	Spec IPAllocationSpec `json:"spec,omitempty"`

	// Observed state of the IP allocation.
	Status IPAllocationStatus `json:"status,omitempty"`
}

// IPAllocationSpec defines the desired ip family, pool or address to allocate for a service.
// Exactly one and no more of the fields must be specified.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.ipFamily) && self.ipFamily != \"\") || (has(self.poolName) && self.poolName != \"\") || (has(self.address) && self.address != \"\")",message="Either ipFamily, poolName or address must be specified"
// +kubebuilder:validation:XValidation:rule="[(has(self.ipFamily) && self.ipFamily != \"\"), (has(self.poolName) && self.poolName != \"\"), (has(self.address) && self.address != \"\")].filter(x, x).size() <= 1",message="Only one of ipFamily, poolName or address can be specified"
type IPAllocationSpec struct {
	// Desired IP family of the address to allocate. Must be either "IPv4" or "IPv6".
	// +kubebuilder:validation:Enum=IPv4;IPv6
	// +optional
	IPFamily corev1.IPFamily `json:"ipFamily,omitempty"`

	// Name of the desired IP pool to allocate an address from.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9\-\.]*[a-z0-9])?$`
	// +optional
	PoolName string `json:"poolName,omitempty"`

	// Specific IP address to allocate. Must be a valid IPv4 or IPv6 address.
	// +optional
	Address string `json:"address,omitempty"`
}

// IPAllocationPhase defines the current phase of the IPAllocation.
type IPAllocationPhase string

const (
	// The IP allocation was not yet processed by the controller. This is the initial
	// phase after creation.
	IPAllocationPhasePending IPAllocationPhase = "Pending"
	// An IP address has been successfully allocated for the service. If the address
	// is not configured for advertisement, this is the final phase. Otherwise, the
	// controller will attempt to program the address on the router and transition to
	// "Programmed".
	IPAllocationPhaseAllocated IPAllocationPhase = "Allocated"
	// The allocated IP address has been successfully programmed for advertisement on
	// the router. This is the final phase for advertised addresses.
	IPAllocationPhaseProgrammed IPAllocationPhase = "Programmed"
	// The controller failed to allocate an IP address for the service or to program
	// the allocated address on the router. This is a terminal phase.
	IPAllocationPhaseFailed IPAllocationPhase = "Failed"
)

// IPAllocationStatus defines the observed state of IPAllocation
// +kubebuilder:object:generate=true
type IPAllocationStatus struct {
	// Phase is the current status phase of the allocation.
	// +optional
	// +kubebuilder:default=Pending
	Phase IPAllocationPhase `json:"phase,omitempty"`

	// Address is the allocated IP address. This is only set if Phase is "Allocated"
	// or "Programmed".
	// +optional
	Address string `json:"address,omitempty"`

	// Whether the allocated address is configured for advertisement via ARP/NDP on
	// the router. This is only set if Phase is "Allocated" or "Programmed".
	// +optional
	Advertised bool `json:"advertised,omitempty"`

	// InterfaceName is the name of the network interface the allocated address is
	// advertised on. This is only definitly set in phase "Programmed" and might be
	// set in phase "Allocated".
	// +optional
	InterfaceName string `json:"interfaceName,omitempty"`

	// Conditions hold the latest available observations of the IPAllocation's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller for
	// this IPAllocation. It is used to detect if the spec has been updated since the
	// last reconciliation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

const (
	ConditionTypeReady      = "Ready"
	ConditionTypeAllocated  = "Allocated"
	ConditionTypeProgrammed = "Programmed"
)

// +kubebuilder:object:root=true
type IPAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPAllocation{}, &IPAllocationList{})
}
