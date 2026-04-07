---
title: API Reference
outline: [2, 4]
---

# API Reference

## Packages

| Group / Version | Description |
| --- | --- |
| [mikrolb.de/v1alpha1](#mikrolbdev1alpha1) | Initial release of the MikroLB API, providing IPPool and IPAllocation resources.  |


## mikrolb.de/v1alpha1

Initial release of the MikroLB API, providing IPPool and IPAllocation resources. 


<div class="details custom-block">
<p><b>Resource Types</b></p>
<ul>
<li><a href="#ipallocation">IPAllocation</a></li>
<li><a href="#ippool">IPPool</a></li>
</ul>
</div>



<div class="crd-type">

### IPAllocation <Badge type="info" text="Kind" />

IPAllocation tracks the allocation of a single IP address for a service. The service reference is stored in the labels `mikrolb.de/service-namespace` and `mikrolb.de/service-name`.



| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `mikrolb.de/v1alpha1` | | |
| `kind` _string_ | `IPAllocation` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | |  |
| `spec` _[IPAllocationSpec](#ipallocationspec)_ | Desired state of the IP allocation. | |  |
| `status` _[IPAllocationStatus](#ipallocationstatus)_ | Observed state of the IP allocation. | |  |


</div>

<div class="crd-type">

### IPAllocationPhase

IPAllocationPhase defines the current phase of the IPAllocation.

<small><strong>Appears in:</strong> [IPAllocationStatus](#ipallocationstatus)<br><strong>Underlying type:</strong> _string_</small>


| Value | Description |
| --- | --- |
| `Pending` | The IP allocation was not yet processed by the controller. This is the initial phase after creation.  |
| `Allocated` | An IP address has been successfully allocated for the service. If the address is not configured for advertisement, this is the final phase. Otherwise, the controller will attempt to program the address on the router and transition to "Programmed".  |
| `Programmed` | The allocated IP address has been successfully programmed for advertisement on the router. This is the final phase for advertised addresses.  |
| `Failed` | The controller failed to allocate an IP address for the service or to program the allocated address on the router. This is a terminal phase.  |


</div>

<div class="crd-type">

### IPAllocationSpec

IPAllocationSpec defines the desired ip family, pool or address to allocate for a service. Exactly one and no more of the fields must be specified.

<small><strong>Appears in:</strong> [IPAllocation](#ipallocation)</small>


| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipFamily` _[IPFamily](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#ipfamily-v1-core)_ | Desired IP family of the address to allocate. Must be either "IPv4" or "IPv6". | |  <Badge type="tip" text="Optional" /><br>Enum: [IPv4 IPv6]<br> |
| `poolName` _string_ | Name of the desired IP pool to allocate an address from. | |  <Badge type="tip" text="Optional" /><br>MaxLength: 253<br>Pattern: `^[a-z0-9]([a-z0-9\-\.]*[a-z0-9])?$`<br> |
| `address` _string_ | Specific IP address to allocate. Must be a valid IPv4 or IPv6 address. | |  <Badge type="tip" text="Optional" /><br> |


</div>

<div class="crd-type">

### IPAllocationStatus

IPAllocationStatus defines the observed state of IPAllocation

<small><strong>Appears in:</strong> [IPAllocation](#ipallocation)</small>


| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[IPAllocationPhase](#ipallocationphase)_ | Phase is the current status phase of the allocation. |`Pending` |  <Badge type="tip" text="Optional" /><br> |
| `address` _string_ | Address is the allocated IP address. This is only set if Phase is "Allocated" or "Programmed". | |  <Badge type="tip" text="Optional" /><br> |
| `advertised` _boolean_ | Whether the allocated address is configured for advertisement via ARP/NDP on the router. This is only set if Phase is "Allocated" or "Programmed". | |  <Badge type="tip" text="Optional" /><br> |
| `interfaceName` _string_ | InterfaceName is the name of the network interface the allocated address is advertised on. This is only definitly set in phase "Programmed" and might be set in phase "Allocated". | |  <Badge type="tip" text="Optional" /><br> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#condition-v1-meta) array_ | Conditions hold the latest available observations of the IPAllocation's state. | |  <Badge type="tip" text="Optional" /><br> |
| `observedGeneration` _integer_ | ObservedGeneration is the most recent generation observed by the controller for this IPAllocation. It is used to detect if the spec has been updated since the last reconciliation. | |  <Badge type="tip" text="Optional" /><br> |


</div>

<div class="crd-type">

### IPPool <Badge type="info" text="Kind" />

IPPool represents a pool of IP addresses that can be allocated to services. It defines the IP family, the list of addresses or CIDRs in the pool, and other configuration options. The status tracks the total number of addresses in the pool and how many are currently allocated.



| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `mikrolb.de/v1alpha1` | | |
| `kind` _string_ | `IPPool` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. | |  |
| `spec` _[IPPoolSpec](#ippoolspec)_ | Desired state of the IP pool. | |  |
| `status` _[IPPoolStatus](#ippoolstatus)_ | Observed state of the IP pool. | |  |


</div>

<div class="crd-type">

### IPPoolSpec

IPPoolSpec defines the desired state of an IP pool, including the IP family, the list of addresses or CIDRs in the pool, and other configuration options.

<small><strong>Appears in:</strong> [IPPool](#ippool)</small>


| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipFamily` _[IPFamily](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#ipfamily-v1-core)_ | IP family of the addresses in this pool. Must be either "IPv4" or "IPv6". | |  <Badge type="danger" text="Required" /><br>Enum: [IPv4 IPv6]<br> |
| `addresses` _string array_ | List of CIDRs, ranges or individual IPs, e.g. "192.168.1.0/24", "10.0.0.5", "172.16.10.7-172.16.10.10" or "fd37:274a:df59::/64". You can use the prefix "!" to exclude specific addresses or ranges from the pool, e.g. "!10.1.2.3" or "!10.1.0.0/24". Exclusions take precedence over inclusions, so if an IP matches both an inclusion and an exclusion, it will be excluded. | |  <Badge type="danger" text="Required" /><br>MinItems: 1<br> |
| `autoAssign` _boolean_ | If `false`, IPs from this pool will not be auto-assigned to services. Services must explicitly request this pool via annotation. |`true` |  |
| `avoidBuggyIPs` _boolean_ | If `true`, IPs from this pool will be allocated in a way that avoids known buggy addresses (e.g. .0, .1, .255 in IPv4 subnets). This only has an effect if the pool contains CIDR blocks, and is ignored for explicitly listed IPs or ranges. Enabling this may reduce the number of usable addresses in small subnets. |`true` |  |
| `advertise` _boolean_ | Whether IPs from this pool should be advertised via ARP/NDP on the router. If `false`, addresses are allocated, load balancers and SNAT are configured, but the address will NOT be configured on an interface in RouterOS. |`true` |  |
| `interfaceName` _string_ | Network interface the router will use for ARP/NDP advertisement. If empty, no interface hint is passed and the most suitable interface is determined by the router's routing table. | |  <Badge type="tip" text="Optional" /><br>Pattern: `^(\|[a-zA-Z0-9][a-zA-Z0-9\-\.]*[a-zA-Z0-9])$`<br> |


</div>

<div class="crd-type">

### IPPoolStatus

IPPoolStatus defines the observed state of an IP pool, including the total number of addresses in the pool, how many are currently allocated, and any relevant conditions.

<small><strong>Appears in:</strong> [IPPool](#ippool)</small>


| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `totalAddresses` _string_ | Total number of allocatable addresses in this pool in a human-readable format, e.g. "256", "1K" or "1M". | |  <Badge type="tip" text="Optional" /><br> |
| `allocatedAddresses` _string_ | Number of currently allocated addresses in a human-readable format, e.g. "256", "1K" or "1M". | |  <Badge type="tip" text="Optional" /><br> |
| `freeAddresses` _string_ | Number of currently unallocated addresses in a human-readable format, e.g. "256", "1K" or "1M". | |  <Badge type="tip" text="Optional" /><br> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#condition-v1-meta) array_ | Conditions represent the latest available observations of the IP pool's state. | |  <Badge type="tip" text="Optional" /><br> |
| `observedGeneration` _integer_ | ObservedGeneration is the most recent generation observed by the controller for this IPPool. It is used to detect if the spec has been updated since the last reconciliation. | |  <Badge type="tip" text="Optional" /><br> |


</div>

