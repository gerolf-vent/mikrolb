# Configuration

The MikroLB controller reads it's runtime configuration completly from environment variables.

## RouterOS

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ROUTEROS_URL` | Yes | - | RouterOS URL containing just scheme and hostname. |
| `ROUTEROS_USERNAME` | Yes | - | RouterOS username |
| `ROUTEROS_PASSWORD` | Yes | - | RouterOS password |
| `ROUTEROS_CA_CERT` | No | - | PEM encoded CA certificate for TLS vertification (not a filepath) |

## Kubernetes

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `LOAD_BALANCER_CLASS_NAME` | No | `mikrolb.de/controller` | Load balancer class to match in K8s services |
| `LOAD_BALANCER_DEFAULT` | No | `false` | Whether MikroLB is the default load balancer for K8s services |

## Internal/Development

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `METRICS_ADDR` | No | `:8080` | Metrics bind address |
| `PROBE_ADDR` | No | `:8081` | Health/readiness probe bind address |
| `WEBHOOK_PORT` | No | `9443` | Webhook server port |
| `WEBHOOK_CERT_DIR` | No | `/mnt/k8s-webhook-server/serving-certs` | Webhook certificate directory |
| `ALLOCATION_TIMEOUT` | No | `5m` | Timeout for internal allocation state, not yet synced with the K8s API |
