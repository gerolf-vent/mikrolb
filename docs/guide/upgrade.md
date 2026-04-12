# Upgrade

## v0.1.1

MikroLB now supports gratuitous advertisements when assigning ip addresses to interfaces. This requires the policies `sniff` and `test` to be granted to the `mikrolb` group and the traffic generator to be enabled on the router. 

```sh
/user/group/set mikrolb policy="read,write,rest-api,api,sniff,test"
/system/device-mode/update traffic-gen=yes
```

You have to reset your RouterOS device after updating the device mode to make the change effective. Run `/system/device-mode/print` to see whether the update worked.

If one of the required policies is missing or the traffic generator is disabled, no gratuitous advertisements will be emitted (without any error).
