# Debugging IPAllocations

`IPAllocation` is the best place to debug why address assignment or programming failed for a Service.

## What To Check First

`IPAllocation` is cluster-scoped, so there is no namespace on the object itself.

Start with a quick overview:

```sh
kubectl get ipallocations.mikrolb.de
```

This already shows key state columns, including:

- configured family, pool, and requested address
- current allocated address
- `Status` (phase)
- `Ready`, `Reason`, and `Message`

Then inspect one object in detail:

```sh
kubectl describe ipallocation <name>
```

The describe output includes both conditions and Kubernetes events, which usually contain the most actionable error details.

## Find IPAllocations For A Service

MikroLB stores the service reference in labels:

- `mikrolb.de/service-namespace`
- `mikrolb.de/service-name`

Use selectors to filter allocations for one Service:

```sh
kubectl get ipallocations.mikrolb.de \
  -l mikrolb.de/service-namespace=default,mikrolb.de/service-name=demo
```

## Read The Status Model

Important status fields and conditions:

- `status.phase`: `Pending`, `Allocated`, `Programmed`, or `Failed`
- `status.conditions[type=Allocated]`: whether an address could be chosen
- `status.conditions[type=Programmed]`: whether router programming succeeded
- `status.conditions[type=Ready]`: overall readiness and primary reason

In practice:

- `Allocated` means address selection succeeded
- `Programmed` means advertisement/programming on RouterOS succeeded
- `Ready=True` means allocation is usable for service traffic

## Common Failure Reasons

The `Reason` and `Message` fields on conditions and events point to the exact failure class.

### Allocation-stage failures

- `AddressInvalid`: the requested `spec.address` is not a valid IP
- `AddressAlreadyUsed`: address already owned by another allocation
- `AddressNotInPool`: requested address does not belong to allowed pool(s)
- `PoolNotFound`: requested `spec.poolName` does not exist
- `PoolIPFamilyMismatch`: requested family does not match pool family
- `PoolExhausted`: no free address in the selected pool (or any auto-assign pool)

### Programming-stage failures

- `ProgrammingFailed` event with `BackendError` condition reason:
  MikroLB allocated an address, but RouterOS programming failed

## Suggested Debug Flow

1. Verify Service selection and annotations:

```sh
kubectl -n <service-namespace> describe service <service-name>
```

2. List allocations for that service:

```sh
kubectl get ipallocations.mikrolb.de \
  -l mikrolb.de/service-namespace=<service-namespace>,mikrolb.de/service-name=<service-name>
```

3. Inspect failing allocation details and events:

```sh
kubectl describe ipallocation <allocation-name>
```

4. Correlate with pool state:

```sh
kubectl get ippool
kubectl describe ippool <pool-name>
```

5. If allocation succeeded but programming failed, inspect controller logs:

```sh
kubectl -n mikrolb-system logs deploy/mikrolb-controller
```

## Typical Fixes By Symptom

- `PoolNotFound`: create the missing pool or fix the pool name annotation
- `PoolIPFamilyMismatch`: align service request family with pool `ipFamily`
- `PoolExhausted`: expand pool ranges/CIDRs or add another pool
- `AddressNotInPool`: request an address that is actually inside the selected pool
- `AddressAlreadyUsed`: choose a different explicit IP or let MikroLB auto-assign
- `BackendError`/`ProgrammingFailed`: verify RouterOS connectivity, credentials, and advertisement interface

## See Also

- [Services](./services)
- [IP Pools](./ip-pools)
- [API Reference: IPAllocation](../reference/api#ipallocation)