# Services

MikroLB uses annotations on Kubernetes `Service` objects to decide how to allocate and program addresses. A service is handled by MikroLB if it uses the MikroLB load balancer class, if it relies on the default load balancer class while that mode is enabled, or if it sets `mikrolb.de/snat-ips`. The annotations let you choose which IPs or pools to use, and whether load balancing or SNAT should be (temporarily) disabled for a service.

## Basic Setup

A MikroLB-managed Service must use the MikroLB load balancer class and be created as a `LoadBalancer` service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
spec:
  type: LoadBalancer
  loadBalancerClass: mikrolb.de/controller
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 8080
```

From there, add annotations to control how MikroLB allocates addresses.

::: info
If the controller is configured as the default load balancer, a `LoadBalancer` Service without an explicit `loadBalancerClass` is also handled by MikroLB.
:::

## Load Balancer Annotations

`mikrolb.de/load-balancer-enabled` controls whether the service should receive load balancer programming at all.

- `true` or omitted: MikroLB programs the service normally
- `false`: MikroLB still allocates addresses, but does not create load balancer rules

`mikrolb.de/load-balancer-ips` requests specific load balancer IP addresses. Use a comma-separated list of IPv4 and IPv6 addresses.

```yaml
metadata:
  annotations:
    mikrolb.de/load-balancer-ips: 192.0.2.10,2001:db8:1234:5678::10
```

`mikrolb.de/load-balancer-pools` requests one IP from each named pool.

```yaml
metadata:
  annotations:
    mikrolb.de/load-balancer-pools: external-v4,external-v6
```

If you omit both `load-balancer-ips` and `load-balancer-pools`, MikroLB can choose a pool automatically when the matching pool has `autoAssign: true`.

## SNAT Annotations

`mikrolb.de/snat-enabled` controls whether MikroLB should configure SNAT for the service.

- `true` or omitted: SNAT is configured normally
- `false`: MikroLB still allocates addresses, but does not create SNAT rules

`mikrolb.de/snat-ips` requests the SNAT address or addresses for the service.

- Use one IPv4 address and one IPv6 address if you want to set them explicitly
- Use `use-lb-ips` if you want the SNAT address to match the allocated load balancer IPs

```yaml
metadata:
  annotations:
    mikrolb.de/snat-ips: use-lb-ips
```

::: tip
You can also use the `mikrolb.de/snat-ips` and `mikrolb.de/snat-enabled` annotations on services that don't have the type `LoadBalancer` or match the `loadBalancerClass` of MikroLB. This will not interfere with other load balancers in Kubernetes.
:::

## Common Patterns

### Use a Specific Pool

If you want a service to always use one pool, request that pool explicitly:

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

### Use Explicit IPs

If you already know the address you want, request it directly:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
  annotations:
    mikrolb.de/load-balancer-ips: 192.0.2.10
    mikrolb.de/snat-ips: use-lb-ips
spec:
  type: LoadBalancer
  loadBalancerClass: mikrolb.de/controller
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 8080
```

### Dual-Stack Service

For dual-stack, combine one IPv4 pool and one IPv6 pool, then request both:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
  annotations:
    mikrolb.de/load-balancer-pools: external-v4,external-v6
    mikrolb.de/snat-ips: use-lb-ips
spec:
  type: LoadBalancer
  loadBalancerClass: mikrolb.de/controller
  ipFamilyPolicy: RequireDualStack
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 8080
```

## What To Expect

When MikroLB processes the service, it creates `IPAllocation` objects behind the scenes. `IPAllocation` is cluster-scoped, so it is not namespaced. You can inspect them with:

```sh
kubectl get ipallocations.mikrolb.de
kubectl describe service demo
```

If the service does not get an address, check that:

- the `loadBalancerClass` matches the controller configuration
- a matching `IPPool` exists
- the pool allows auto-assignment or the service explicitly names the pool
- the requested IP family matches the pool family

Allocation errors are surfaced on the `IPAllocation` itself. `kubectl get ipallocations.mikrolb.de` shows the current `Status`, `Reason`, and `Message` columns, and `kubectl describe ipallocation <name>` shows the same state together with the Kubernetes Events for the object.

## See Also

- [IP Pools](./ip-pools)
- [Service Annotations](../reference/annotations)