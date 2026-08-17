# Changelog

All notable changes to this project will be documented in this file.

## [v0.1.2] - 2026-08-17

### Security-Fixes

#### Forward chain accepted all traffic

<table>
<tr><td><strong>Issue</strong></td><td>The mangle rule in the RouterOS backend was missing the destination address-list filter, causing it to mark every forwarded connection, not just those addressed to a load balancer IP, with the <code>mikrolb-lb-connection</code> connection mark.</td></tr>
<tr><td><strong>Impact</strong></td><td>Because the <code>forward</code> chain accept rule trusts this mark, the firewall accepted all forwarded traffic instead of only traffic destined for load balancer IPs, effectively disabling the intended traffic restriction.</td></tr>
<tr><td><strong>Severity</strong></td><td>High. Any device with a MikroLB-managed router was exposed to unrestricted forwarded traffic.</td></tr>
<tr><td><strong>Fix</strong></td><td>Upgrade to this version and let the controller reconcile; the existing rule on the router is patched automatically. See the <a href="https://mikrolb.de/guide/upgrade#v012">upgrade guide</a> for details, including how to apply the fix without waiting for the next reconcile.</td></tr>
</table>

## [v0.1.1] - 2026-04-12

### Added

- Gratuitous ARP advertisements support in the RouterOS backend
- Policies fetcher to the RouterOS API client
- Policies check in the backend for gratuitous advertisements

### Fixed

- Mangle rule in RouterOS backend
- Missing image tag in Kustomization
- Missing labels in Kustomization
- Go lint warning for slice length check

### Improved

- Documentation (getting started and installation guides, README)

## [v0.1.0] - 2026-04-07

Initial release of MikroLB, a Kubernetes controller that turns a MikroTik RouterOS v7 device into a `LoadBalancer` provider for your cluster.

### Added

#### Core controller
- `mikrolb-controller` command as the controller entrypoint
- Service reconciler that allocates external IPs and programs RouterOS load balancing rules
- `IPPool` and `IPAllocation` reconcilers
- Validating webhook for `IPPool` resources
- API v1alpha1 with `IPPool` and `IPAllocation` types
- Configuration type for controller settings

#### RouterOS integration
- Backend interface and shared service type
- RouterOS v7 backend implementation
- HTTPS REST API client and mock server for RouterOS
- IP range (set) implementations for pool address tracking

#### Utilities
- Helper for generating unique hashes from strings
- Helper for metric number formatting

#### Packaging and deployment
- Container image build
- Kustomization for deploying the controller
- Makefile target for manifest generation
- Improved container image build in the Makefile

#### Documentation
- Documentation site source
- Guide pages
- Configuration and annotation reference
- API reference
- Project README

#### CI/CD
- GitHub workflow for building and publishing the container image
- GitHub workflow for deploying the documentation site

### Fixed
- `setup-envtest` target in the Makefile
