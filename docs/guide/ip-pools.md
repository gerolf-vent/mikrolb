# IP Pools

IP pools define which addresses MikroLB can hand out to Services. A pool is a cluster-scoped `IPPool` resource that describes the IP family, the available addresses, and how those addresses should be treated by the controller and the router.

## Pool Basics

Each pool needs:

- an `ipFamily` of `IPv4` or `IPv6`
- at least one address entry in `addresses`
- a unique cluster-wide name in `metadata.name`

The `addresses` list can contain:

- a CIDR block, such as `192.0.2.0/24`
- a single address, such as `192.0.2.10`
- an explicit range, such as `192.0.2.10-192.0.2.25`
- exclusions prefixed with `!`, such as `!192.0.2.13` or `!192.0.2.0/30`

Exclusions win over inclusions. If an address is matched by both, it will not be part of the pool.

## Create a Pool

Create an IP pool by applying an `IPPool` manifest:

```yaml
apiVersion: mikrolb.de/v1alpha1
kind: IPPool
metadata:
  name: external-v4
spec:
  ipFamily: IPv4
  addresses:
    - 192.0.2.10-192.0.2.25
    - '!192.0.2.13'
    - '!192.0.2.20'
  autoAssign: true
  avoidBuggyIPs: true
  advertise: true
  interfaceName: ether1
```

Apply it with:

```sh
kubectl apply -f external-v4.yaml
kubectl get ippool
```

## Configuration Fields

`autoAssign` controls whether the pool can be used automatically. When it is `true`, MikroLB may choose the pool without an explicit request from the Service. When it is `false`, Services must request the pool by name.

`advertise` controls whether MikroLB asks RouterOS to advertise the address on an interface. Set it to `false` when you want MikroLB to allocate the address and configure load balancing, but not add the address to the router interface.

`interfaceName` is optional. If you set it, MikroLB uses that interface for ARP or NDP advertisement. If you leave it empty, MikroTik will try to choose one based on the routing table in RouterOS.

`avoidBuggyIPs` is useful for CIDR-based pools. It skips addresses that are commonly problematic in subnets, such as `.0`, `.1`, and `.255` in IPv4 ranges. This does not affect explicitly listed IPs or ranges.

## Requesting a Specific Pool

If you want a Service to use a particular pool, set the `mikrolb.de/load-balancer-pools` annotation:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
  annotations:
    mikrolb.de/load-balancer-pools: external-v4
spec:
  type: LoadBalancer
  loadBalancerClass: mikrolb.de/controller
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 8080
```

You can list multiple pool names, separated by commas. MikroLB will request one IP from each pool listed.

## Status

After reconciliation, the pool status shows how many addresses exist and how many are already allocated. The counts are reported in a human-readable format, so MikroLB may display larger values as compact units such as `K` for thousands or `M` for millions:

- `totalAddresses`
- `allocatedAddresses`
- `freeAddresses`

You can inspect the full object with:

```sh
kubectl describe ippool external-v4
```

## Dual-Stack Setup

For dual-stack services, create one IPv4 pool and one IPv6 pool. Keep the pools separate and request both from the Service when needed.

```yaml
apiVersion: mikrolb.de/v1alpha1
kind: IPPool
metadata:
  name: external-v6
spec:
  ipFamily: IPv6
  addresses:
    - 2001:db8:1234:5678::/64
  autoAssign: true
  advertise: true
```

## Common Pitfalls

- The `ipFamily` must match the addresses in the pool.
- `addresses` is required and must not be empty.
- Pool names are cluster-wide, not namespaced.
- If `autoAssign` is `false`, Services must request the pool explicitly.
- If a CIDR is too small and `avoidBuggyIPs` is enabled, fewer addresses may be available than expected.

## See also

- [API Reference: IPPool](../reference/api#ippool)
