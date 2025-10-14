# GitHub Copilot Instructions for ActiveMQ Artemis Operator

## Project Context

This is a Kubernetes operator for Apache ActiveMQ Artemis, written in Go using the controller-runtime framework.

## Documentation

Reference these AI-optimized documentation files in `AI_documentation/`:

1. **AI_KNOWLEDGE_INDEX.yaml** - Concept lookup and code navigation
2. **operator_architecture.md** - Complete technical architecture (22 subsystems)
3. **operator_conventions.md** - Naming conventions, defaults, and magic behaviors
4. **contribution_guide.md** - Development workflow and testing guide

## Code Patterns

**Controller Pattern:**
- Reconcile functions in `controllers/`
- Resource generation in `pkg/resources/`
- Utilities in `pkg/utils/`

**Key Concepts:**
- `reconciliation_loop`: controllers/activemqartemis_controller.go::Reconcile
- `statefulset_management`: pkg/resources/statefulsets/
- `validation_chain`: controllers/activemqartemis_reconciler.go::validate
- `broker_properties`: Configuration via properties files
- `restricted_mode`: Security-sensitive deployment mode

**Validation:**
- Chain validators together (see operator_architecture.md "Validation Architecture")
- Return early on errors
- Update status conditions: Valid, Deployed, Ready, ConfigApplied

**Naming Conventions:**
- Resources: `{cr-name}-{resource-type}-{ordinal}`
- Functions: Clear, descriptive names
- Constants: ALL_CAPS with descriptive names

## Test-Driven Development

**Required for all changes:**
- Write tests for new functionality
- Unit tests: `go test ./controllers -run <TestName>`
- E2E tests: `USE_EXISTING_CLUSTER=true go test -v ./controllers -ginkgo.focus="<pattern>" -timeout 10m`
- Code is NOT done until tests pass

**Common test patterns:**
- Check existing tests in `controllers/*_test.go`
- Use Ginkgo/Gomega for E2E tests
- Follow TDD approach: test first, then implement

**E2E Test Environment Setup:**

Complete minikube setup (required for restricted mode tests):

```bash
# 1. Start minikube with dedicated profile and set as active
minikube start --profile cursor --memory=4096 --cpus=2
minikube profile cursor
kubectl config use-context cursor

# 2. Enable ingress addon (REQUIRED for restricted mode tests)
minikube addons enable ingress

# 3. Wait for ingress controller
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s

# 4. Enable SSL passthrough (CRITICAL for mTLS in restricted mode)
kubectl patch deployment ingress-nginx-controller -n ingress-nginx \
  --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--enable-ssl-passthrough"}]'

# 5. Wait for controller restart
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx

# 6. Verify cert-manager (auto-installed by tests if missing)
kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/instance=cert-manager \
  -n cert-manager --timeout=120s 2>/dev/null || echo "cert-manager will be auto-installed"
```

**Run tests:**
```bash
USE_EXISTING_CLUSTER=true go test -v ./controllers -ginkgo.focus="<test-name>" -timeout 10m
```

**Cleanup:**
```bash
minikube stop --profile cursor
minikube delete --profile cursor
```

See `contribution_guide.md` (lines 850-889) for complete details.

## Magic Behaviors

From operator_conventions.md:

**ExtraMount Suffixes** (automatic detection):
- `-logging-config`: Custom Log4j configuration
- `-jaas-config`: JAAS authentication configuration  
- `-bp`: Broker properties configuration

**Platform Detection:**
- Automatic OpenShift vs Kubernetes detection
- Route (OpenShift) vs Ingress (Kubernetes) selection

**Configuration Precedence:**
1. CR spec fields (highest priority)
2. Environment variables
3. Operator defaults (lowest priority)

## Status Management

Update CR status conditions in reconciliation:
- Use `meta.SetStatusCondition()` to update conditions
- Condition types: ValidConditionType, DeployedConditionType, ReadyConditionType
- See api/v1beta1/activemqartemis_types.go for status definitions

## Common Integration Points

**Environment Variables:**
- `JAVA_ARGS_APPEND`: Append to Java arguments
- `JDK_JAVA_OPTIONS`: JDK options
- Define constants in activemqartemis_reconciler.go

**Resource Naming:**
- Use namer.NamerData for consistent naming
- Pattern: Prefix(cr-name).Base(feature).Suffix(type).Generate()

## Key Files

**Controllers:**
- activemqartemis_controller.go - Main controller, Reconcile entry
- activemqartemis_reconciler.go - Reconciliation logic (Process*, validate)
- activemqartemisscaledown_controller.go - Message migration

**API:**
- api/v1beta1/activemqartemis_types.go - CR type definitions

**Resources:**
- pkg/resources/statefulsets/ - StatefulSet generation
- pkg/resources/services/ - Service generation
- pkg/resources/pods/ - Pod template configuration

**Utilities:**
- pkg/utils/common/common.go - Common utilities, version resolution
- pkg/utils/common/conditions.go - Status condition management

## Important

- Documentation is FOR AI assistants, not BY AI assistants
- NEVER create new .md files unless explicitly requested
- NEVER add to AI_documentation/ directory
- Follow TDD: tests must pass before code is considered complete
