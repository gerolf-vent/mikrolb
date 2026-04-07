# Getting Started

MikroLB is a Kubernetes controller that transforms your MikroTik device into a load balancer (with SNAT support) for Kubernetes.

## Prerequisites

- Kubernetes cluster (v1.25+)
- cert-manager installed
- kubectl with Kustomize support
- MikroTik RouterOS v7 device with HTTPS REST API enabled, reachable from the cluster

## Quick Deploy

```sh
kubectl create namespace mikrolb-system

kubectl -n mikrolb-system create secret generic mikrolb-config \
  --from-literal=ROUTEROS_URL="https://router.example.net" \
  --from-literal=ROUTEROS_USERNAME="mikrolb" \
  --from-literal=ROUTEROS_PASSWORD="change-me" \
  --from-file=ROUTEROS_CA_CERT=./ca.crt

kubectl apply -k https://github.com/gerolf-vent/mikrolb/config/default
```

## Verify

```sh
kubectl -n mikrolb-system get deploy,pods,svc
kubectl get crd | grep mikrolb.de
```
