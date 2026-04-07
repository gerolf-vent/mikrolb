# Service Annotations

MikroLB supports the following Service annotations:

| Annotation | Description |
| --- | --- |
| `mikrolb.de/load-balancer-enabled` | If set to `false`, the load balancer will be disabled for this service, but address(es) will still be allocated. |
| `mikrolb.de/load-balancer-ips` | Request specific LoadBalancer IP address(es). An arbitrary number of IPv4 and IPv6 addresses are supported. |
| `mikrolb.de/load-balancer-pools` | Request one IP address from every pool specified. |
| `mikrolb.de/snat-enabled` | If set to `false`, the SNAT will be disabled for this service, but address(es) will still be allocated. |
| `mikrolb.de/snat-ips` | Request these IP address(es) for SNAT. Only one IPv4 and one IPv6 address are supported. Use the special value `use-lb-ips` to use the allocated load balancer IP address(es) for SNAT. |

## Example

```yaml
apiVersion: v1
kind: Service
metadata:
  name: demo
  annotations:
    mikrolb.de/load-balancer-pools: external-v4,external-v6
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
