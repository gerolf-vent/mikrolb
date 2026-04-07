# MikroLB
[![Release](https://img.shields.io/github/v/release/gerolf-vent/mikrolb)](https://github.com/gerolf-vent/mikrolb/releases)
[![Docs](https://img.shields.io/badge/docs-mikrolb.de-blue)](https://mikrolb.de)
[![License](https://img.shields.io/github/license/gerolf-vent/mikrolb)](https://github.com/gerolf-vent/mikrolb/blob/main/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/gerolf-vent/mikrolb)](https://goreportcard.com/report/github.com/gerolf-vent/mikrolb)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gerolf-vent/mikrolb)](https://github.com/gerolf-vent/mikrolb)
[![Last Commit](https://img.shields.io/github/last-commit/gerolf-vent/mikrolb)](https://github.com/gerolf-vent/mikrolb/commits/main)

MikroLB is a Kubernetes controller that turns a MikroTik RouterOS v7 device into a `LoadBalancer` provider for your cluster. It allocates external IPs from cluster-scoped pools and programs the router (load balancing rules, optional SNAT, and address advertisement) through the RouterOS HTTPS REST API.

Full documentation lives at **[mikrolb.de](https://mikrolb.de)**.

## Features

- **Kubernetes-native** — manage address pools and allocations via CRDs (`IPPool`, `IPAllocation`)
- **RouterOS v7 support** — programs load balancing and SNAT through the HTTPS REST API
- **Dual-stack** — separate IPv4 and IPv6 pools, combined per Service
- **Flexible allocation** — auto-assign from a pool, request a pool by name, or pin specific IPs via annotations
- **Secure by default** — Secret-based configuration with optional custom CA support

## How It Works

1. You create one or more `IPPool` resources describing the IPv4/IPv6 ranges MikroLB may hand out.
2. A user creates a `LoadBalancer` Service with `loadBalancerClass: mikrolb.de/controller` (or relies on the default class).
3. MikroLB allocates an address (tracked as an `IPAllocation`), programs the corresponding load balancing rules on the RouterOS device, and optionally configures SNAT and interface advertisement.

## Prerequisites

- Kubernetes cluster (v1.25+)
- [cert-manager](https://cert-manager.io/) installed
- `kubectl` with Kustomize support
- A MikroTik RouterOS v7 device with the HTTPS REST API enabled and reachable from the cluster

## Quick Start

Prepare a RouterOS user with `read,write,rest-api` policy (see [Installation](https://mikrolb.de/guide/installation) for details), then deploy the controller:

```sh
kubectl create namespace mikrolb-system

kubectl -n mikrolb-system create secret generic mikrolb-config \
  --from-literal=ROUTEROS_URL="https://router.example.net" \
  --from-literal=ROUTEROS_USERNAME="mikrolb" \
  --from-literal=ROUTEROS_PASSWORD="change-me" \
  --from-file=ROUTEROS_CA_CERT=./router-ca.crt

kubectl apply -k https://github.com/gerolf-vent/mikrolb/config/default
```

You might have to adjust the order of firewall rules in RouterOS after MikroLB has run at least once (see [Installation](https://mikrolb.de/guide/installation) for details).

Create an IP pool:

```yaml
apiVersion: mikrolb.de/v1alpha1
kind: IPPool
metadata:
  name: external-v4
spec:
  ipFamily: IPv4
  addresses:
    - 192.0.2.10-192.0.2.25
  autoAssign: true
  advertise: true
  interfaceName: ether1
```

Then expose a workload:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
  annotations:
    mikrolb.de/load-balancer-pools: external-v4
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

## Configuration

The controller is configured entirely through environment variables (typically supplied by the `mikrolb-config` Secret). The most important ones:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ROUTEROS_URL` | Yes | – | RouterOS URL (scheme + hostname) |
| `ROUTEROS_USERNAME` | Yes | – | RouterOS username |
| `ROUTEROS_PASSWORD` | Yes | – | RouterOS password |
| `ROUTEROS_CA_CERT` | No | – | PEM-encoded CA certificate for TLS verification |
| `LOAD_BALANCER_CLASS_NAME` | No | `mikrolb.de/controller` | Load balancer class to match |
| `LOAD_BALANCER_DEFAULT` | No | `false` | Make MikroLB the default load balancer |

See the [configuration reference](https://mikrolb.de/reference/configuration) for the complete list.

## Documentation

- [Getting Started](https://mikrolb.de/guide/getting-started)
- [Installation](https://mikrolb.de/guide/installation)
- [IP Pools](https://mikrolb.de/guide/ip-pools)
- [Services](https://mikrolb.de/guide/services)
- [Debugging IPAllocations](https://mikrolb.de/guide/debugging-ipallocations)
- [API Reference](https://mikrolb.de/reference/api)
- [Annotation Reference](https://mikrolb.de/reference/annotations)

## License

Licensed under the [Apache License 2.0](LICENSE).
