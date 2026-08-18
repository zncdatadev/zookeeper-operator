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
- github.com/zncdatadev/operator-go - Shared operator framework

### External
- kubebuilder - Project scaffolding
- controller-runtime
- Kubernetes client-go

### AI Worktree Development Mode

**IMPORTANT**: When making code changes, work in a worktree under `.worktree/`, NOT in the main working directory.

#### Workflow
1. Create worktree: `git worktree add .worktree/<branch-name> -b <branch-name>`
2. Work in `.worktree/<branch-name>/` directory
3. Test: `cd .worktree/<branch-name> && make lint && make test`
4. Commit changes in the worktree
5. Push and create PR from the worktree branch
6. Cleanup: `git worktree remove .worktree/<branch-name>`

#### Rules
- NEVER modify files directly in the main working directory
- Each task gets its own worktree with a descriptive branch name
- Run `make generate` if API structs are modified
- Run `make lint && make test` before committing

<!-- MANUAL: -->
