# Upgrade

## v0.1.2

### Security

#### Forward chain accepted all traffic

<table>
<tr><td><strong>Issue</strong></td><td>The mangle rule in the RouterOS backend was missing the destination address-list filter, causing it to mark every forwarded connection, not just those addressed to a load balancer IP, with the <code>mikrolb-lb-connection</code> connection mark.</td></tr>
<tr><td><strong>Impact</strong></td><td>Because the <code>forward</code> chain accept rule trusts this mark, the firewall accepted all forwarded traffic instead of only traffic destined for load balancer IPs, effectively disabling the intended traffic restriction.</td></tr>
<tr><td><strong>Severity</strong></td><td>High. Any device with a MikroLB-managed router was exposed to unrestricted forwarded traffic.</td></tr>
<tr><td><strong>Fix</strong></td><td>Upgrade to this version and let the controller reconcile; the existing rule on the router is patched automatically. To apply the fix immediately without waiting for the next reconcile, restart the <code>mikrolb-controller</code> deployment: <code>kubectl -n mikrolb-system rollout restart deployment mikrolb-controller</code></td></tr>
</table>

## v0.1.1

MikroLB now supports gratuitous advertisements when assigning ip addresses to interfaces. This requires the policies `sniff` and `test` to be granted to the `mikrolb` group and the traffic generator to be enabled on the router. 

```sh
/user/group/set mikrolb policy="read,write,rest-api,api,sniff,test"
/system/device-mode/update traffic-gen=yes
```

You have to reset your RouterOS device after updating the device mode to make the change effective. Run `/system/device-mode/print` to see whether the update worked.

If one of the required policies is missing or the traffic generator is disabled, no gratuitous advertisements will be emitted (without any error).
