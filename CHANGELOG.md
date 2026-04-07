# Changelog

All notable changes to this project will be documented in this file.

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
