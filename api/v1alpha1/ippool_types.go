package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPPool represents a pool of IP addresses that can be allocated to services. It defines the
// IP family, the list of addresses or CIDRs in the pool, and other configuration options. The
// status tracks the total number of addresses in the pool and how many are currently allocated.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ipp
// +kubebuilder:printcolumn:name="Family",type=string,JSONPath=`.spec.ipFamily`
// +kubebuilder:printcolumn:name="Auto-Assign",type=boolean,JSONPath=`.spec.autoAssign`
// +kubebuilder:printcolumn:name="Advertise",type=boolean,JSONPath=`.spec.advertise`
// +kubebuilder:printcolumn:name="Interface",type=string,JSONPath=`.spec.interfaceName`,priority=1
// +kubebuilder:printcolumn:name="Total",type=string,JSONPath=`.status.totalAddresses`,priority=1
// +kubebuilder:printcolumn:name="Allocated",type=string,JSONPath=`.status.allocatedAddresses`,priority=1
// +kubebuilder:printcolumn:name="Free",type=string,JSONPath=`.status.freeAddresses`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Desired state of the IP pool.
	Spec IPPoolSpec `json:"spec,omitempty"`

	// Observed state of the IP pool.
	Status IPPoolStatus `json:"status,omitempty"`
}

// IPPoolSpec defines the desired state of an IP pool, including the IP family, the list
// of addresses or CIDRs in the pool, and other configuration options.
// +kubebuilder:object:generate=true
type IPPoolSpec struct {
	// IP family of the addresses in this pool. Must be either "IPv4" or "IPv6".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=IPv4;IPv6
	IPFamily corev1.IPFamily `json:"ipFamily"`

	// List of CIDRs, ranges or individual IPs, e.g. "192.168.1.0/24", "10.0.0.5",
	// "172.16.10.7-172.16.10.10" or "fd37:274a:df59::/64". You can use the prefix "!"
	// to exclude specific addresses or ranges from the pool, e.g. "!10.1.2.3" or
	// "!10.1.0.0/24". Exclusions take precedence over inclusions, so if an IP matches
	// both an inclusion and an exclusion, it will be excluded.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	Addresses []string `json:"addresses"`

	// If `false`, IPs from this pool will not be auto-assigned to services. Services
	// must explicitly request this pool via annotation.
	// +kubebuilder:default=true
	AutoAssign bool `json:"autoAssign,omitempty"`

	// If `true`, IPs from this pool will be allocated in a way that avoids
	// known buggy addresses (e.g. .0, .1, .255 in IPv4 subnets). This only has
	// an effect if the pool contains CIDR blocks, and is ignored for explicitly
	// listed IPs or ranges. Enabling this may reduce the number of usable
	// addresses in small subnets.
	// +kubebuilder:default=true
	AvoidBuggyIPs bool `json:"avoidBuggyIPs,omitempty"`

	// Whether IPs from this pool should be advertised via ARP/NDP on the
	// router. If `false`, addresses are allocated, load balancers and SNAT are
	// configured, but the address will NOT be configured on an interface in
	// RouterOS.
	// +kubebuilder:default=true
	Advertise bool `json:"advertise,omitempty"`

	// Network interface the router will use for ARP/NDP advertisement. If
	// empty, no interface hint is passed and the most suitable interface is
	// determined by the router's routing table.
	// +kubebuilder:validation:Pattern=`^(|[a-zA-Z0-9][a-zA-Z0-9\-\.]*[a-zA-Z0-9])$`
	// +optional
	InterfaceName string `json:"interfaceName,omitempty"`
}

// IPPoolStatus defines the observed state of an IP pool, including the total number of
// addresses in the pool, how many are currently allocated, and any relevant conditions.
// +kubebuilder:object:generate=true
type IPPoolStatus struct {
	// Total number of allocatable addresses in this pool in a human-readable format,
	// e.g. "256", "1K" or "1M".
	// +optional
	TotalAddresses string `json:"totalAddresses,omitempty"`

	// Number of currently allocated addresses in a human-readable format, e.g.
	// "256", "1K" or "1M".
	// +optional
	AllocatedAddresses string `json:"allocatedAddresses,omitempty"`

	// Number of currently unallocated addresses in a human-readable format, e.g.
	// "256", "1K" or "1M".
	// +optional
	FreeAddresses string `json:"freeAddresses,omitempty"`

	// Conditions represent the latest available observations of the IP pool's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller for this IPPool.
	// It is used to detect if the spec has been updated since the last reconciliation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPPool{}, &IPPoolList{})
}
