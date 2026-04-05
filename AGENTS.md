<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-03-29 | Updated: 2026-03-29 -->

# zookeeper-operator

## Purpose
Manages Apache ZooKeeper deployments on Kubernetes. Handles creation, configuration, and lifecycle management of ZooKeeper clusters for distributed coordination and configuration management.

## Key Files
| File | Description |
|------|-------------|
| `go.mod` | Go module dependencies |
| `Makefile` | Build and development commands |
| `PROJECT` | Kubebuilder project metadata |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `api/` | Kubernetes CRD definitions for ZooKeeper |
| `cmd/` | Operator entry point |
| `config/` | Kubernetes manifests and kustomize configs |
| `internal/` | Controller and reconciliation logic |
| `test/` | E2E test suites |

## For AI Agents

### Working In This Directory
- Standard Kubebuilder operator structure
- Uses operator-go framework for reconciliation
- Run `make test` for unit tests
- Run `make deploy` to deploy to cluster

### Testing Requirements
- E2E tests in test/e2e/
- Requires Kubernetes cluster for testing

### Common Patterns
- Controllers in internal/controller/
- CRDs use v1alpha1 API version
- Follows operator-go GenericReconciler pattern

## Dependencies

### Internal
- ../operator-go - Shared operator framework

### External
- controller-runtime
- Kubernetes client-go

<!-- MANUAL: -->
