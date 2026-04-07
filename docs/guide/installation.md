# Installation

## Prepare the router

### Setup REST API

If you want to use a custom TLS certificate (recommended), you can generate one directly on your router with the following commands. Don't forget to replace `subject-alt-names` with the IPs and DNS names you want to use for connecting to your router. The certificates will be valid for ~30 years.

```sh
/certificate add name=router-ca common-name=router-ca days-valid=10950 key-usage=key-cert-sign,crl-sign
/certificate sign router-ca
/certificate add name=router common-name=router days-valid=10950 subject-alt-name=IP:10.1.0.254,IP:2001:db8:1234:5678:abcd:ef01:2345:6789,DNS:router.example.net
/certificate sign router ca=router-ca
```

Now ensure this certificate is configured and the service (which provides the REST API) is enabled.

```sh
/ip/service/set www-ssl certificate=router
/ip/service/enable www-ssl
```

### Setup user

An user account is required which MikroLB will use for authentication.

```sh
/user/group/add name=mikrolb comment="MikroLB controller" policy=read,write,rest-api
/user/add name=mikrolb comment="MikroLB controller" group=mikrolb
```

## Deploy MikroLB

If you generated a certificate on the router, you have to export it via the following command and download the created file `router-ca.crt`.
```sh
/certificate/export-certificate router-ca file-name=router-ca
```

After you retreived the CA certificate used to sign the TLS certificate your router uses, you can deploy MikroLB from your local system.

```sh
kubectl create namespace mikrolb-system

kubectl -n mikrolb-system create secret generic mikrolb-config \
  --from-literal=ROUTEROS_URL="https://router.example.net" \
  --from-literal=ROUTEROS_USERNAME="mikrolb" \
  --from-literal=ROUTEROS_PASSWORD="change-me" \
  --from-file=ROUTEROS_CA_CERT=./router-ca.crt

kubectl apply -k https://github.com/gerolf-vent/mikrolb/config/default
```

## Adjust router firewall rules

After MikroLB has run at least once, you can see the following rules appear in the firewall on the router, of which you might to have to adjust the ordering.

| Table | Chain | Rule comment | Ordering notice |
| ----- | ----- | ------------ | --------------- |
| filter | input | `mikrolb: reject unmatched LB ports` | First rule or directly after accepting established connections |
| filter | forward | `mikrolb: accept LB connections` | First rule or directly after accepting established connections |
| nat | dstnat | `mikrolb: LB connections` | First rule |
| nat | srcnat | `mikrolb: SNAT connections` | First rule |
| mangle | prerouting | `mikrolb: LB connections` | Does not matter |

::: tip
It's also highly recommended to fasttrack and accept established connections early.

```sh
/ip/firewall/filter add place-before=0 chain=forward action=accept connection-state=established,related comment="accept established connections"
/ip/firewall/filter add place-before=0 chain=forward action=fasttrack-connection connection-state=established,related comment="fasttrack established connections"

/ipv6/firewall/filter add place-before=0 chain=forward action=accept connection-state=established,related comment="accept established connections"
/ipv6/firewall/filter add place-before=0 chain=forward action=fasttrack-connection connection-state=established,related comment="fasttrack established connections"
```

The FastTrack rule should appear before the accept rule. The `place-before=0` inserts both rules at the top of the chain, so the commands have to be executed in this reversed order.
:::
