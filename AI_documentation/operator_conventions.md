This document provides a comprehensive reference of all conventions, defaults, and automatic behaviors implemented by the ActiveMQ Artemis Operator. Understanding these conventions is essential for both using and extending the operator.

**Table of Contents**

*   [Convention Over Configuration Philosophy](#convention-over-configuration-philosophy)
*   [Resource Naming Conventions](#resource-naming-conventions)
*   [Platform Detection and Adaptation Conventions](#platform-detection-and-adaptation-conventions)
*   [Persistence and Storage Conventions](#persistence-and-storage-conventions)
*   [Service Discovery and Networking Conventions](#service-discovery-and-networking-conventions)
*   [Configuration Precedence Conventions](#configuration-precedence-conventions)
*   [Zero-Configuration Deployment Convention](#zero-configuration-deployment-convention)
*   [Suffix-Based Magic Behavior](#suffix-based-magic-behavior)
*   [Mount Path Conventions](#mount-path-conventions)
*   [Environment Variable Patterns](#environment-variable-patterns)
*   [Default Values and Configurations](#default-values-and-configurations)
*   [Broker Version Dependencies](#broker-version-dependencies)
*   [Default Retry and Timeout Conventions](#default-retry-and-timeout-conventions)
*   [Certificate Management Conventions](#certificate-management-conventions)
*   [Annotation and Label Conventions](#annotation-and-label-conventions)
*   [Default Security Conventions](#default-security-conventions)
*   [Constants and Magic Values](#constants-and-magic-values)
*   [Regex Patterns](#regex-patterns)
*   [Hash-Based Resource Management](#hash-based-resource-management)
*   [Volume Mount Ordering](#volume-mount-ordering)

## Convention Over Configuration Philosophy

The ActiveMQ Artemis Operator embodies the **"Convention Over Configuration"** philosophy, providing sensible defaults for everything while allowing customization when needed.

> **Philosophy**: The operator should work out-of-the-box with minimal configuration, following established patterns and conventions that users can rely on without explicit configuration.

**🧪 Tested in**: 
- controllers/broker_name_test.go::broker_name_test.go - Naming convention tests
- controllers/common_util_test.go::common_util_test.go - Utility function and convention testing
- controllers/suite_test.go::suite_test.go - Platform detection and environment setup testing

## Resource Naming Conventions

The operator pkg/utils/namer/namer.go that are controllers/activemqartemis_controller.go the namer package:

### **StatefulSet and Pod Naming**
```go
// Pattern: <cr-name>-ss follows convention
StatefulSet: "ex-aao-ss"            // Generated via CrToSS()
Pods: "ex-aao-ss-0", "ex-aao-ss-1"  // Kubernetes StatefulSet convention
```
pkg/utils/namer/namer.go: `CrToSS()` function  
controllers/activemqartemis_controller.go: Applied in `MakeNamers()`

### **Service Naming**
```go
// Services follow <cr-name>-<type>-<suffix> pattern
HeadlessService: "ex-aao-hdls-svc"  // For StatefulSet DNS
PingService: "ex-aao-ping-svc"      // For cluster discovery
AcceptorService: "ex-aao-artemis-svc" // Per-acceptor exposure
```
controllers/activemqartemis_controller.go: Services are controllers/activemqartemis_controller.go

### **Secret Naming**
```go
// Secrets follow <cr-name>-<purpose>-secret pattern
BrokerProperties: "ex-aao-props"           // Properties storage
Credentials: "ex-aao-credentials-secret"   // Auth credentials
Console: "ex-aao-console-secret"           // Console TLS
Netty: "ex-aao-netty-secret"              // Netty TLS
```
controllers/activemqartemis_reconciler.go: Properties naming via `getPropertiesResourceNsName()`  
controllers/activemqartemis_controller.go: Other secrets controllers/activemqartemis_controller.go

### **Label Conventions**
```go
// Standard labels applied to all resources
Labels: {
    "application": "<cr-name>-app",     // Application grouping
    "ActiveMQArtemis": "<cr-name>"      // Resource association
}
```
pkg/utils/selectors/label.go: Labels are pkg/utils/selectors/label.go with pkg/utils/selectors/label.go

## Platform Detection and Adaptation Conventions

The operator main.go to different Kubernetes platforms:

### **OpenShift vs Kubernetes Detection**
```go
// Auto-detection via API server inspection
isOpenshift = DetectOpenshiftWith(restConfig)

// Platform-specific behavior:
// OpenShift: Uses Routes for external exposure
// Kubernetes: Uses Ingress for external exposure  
// OpenShift: Detects FIPS mode from cluster config
// Kubernetes: Assumes standard compliance
```
pkg/utils/common/common.go

### **Architecture Support**
```go
// Multi-architecture image selection
// Convention: Try arch-specific image first, fall back to generic
RELATED_IMAGE_ActiveMQ_Artemis_Broker_Kubernetes_<version>_<arch>
RELATED_IMAGE_ActiveMQ_Artemis_Broker_Kubernetes_<version>

// Supported architectures: amd64, arm64, s390x, ppc64le
```
pkg/utils/common/common.go: Architecture-specific images are pkg/utils/common/common.go, then pkg/utils/common/common.go

## Persistence and Storage Conventions

```go
// Default storage behavior
persistenceEnabled: false             // Ephemeral by default
storage.size: "2Gi"                  // Default PVC size when enabled
storage.accessMode: "ReadWriteOnce"   // Default access mode

// Convention: No storage class specified = use cluster default
// Convention: PVCs retained on scale-down when messageMigration enabled
// Convention: Each broker pod gets its own PVC in StatefulSet
```
controllers/activemqartemis_reconciler.go: Persistence is controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go for each broker

## Service Discovery and Networking Conventions

```go
// Service naming patterns
HeadlessService: "<cr-name>-hdls-svc"           // Always created
PingService: "<cr-name>-ping-svc"               // Cluster discovery
AcceptorService: "<cr-name>-<acceptor>-<ordinal>-svc"  // Per-acceptor, per-pod

// Route naming (OpenShift)
Route: "<service-name>-rte"                     // Route per service
// Convention: Uses passthrough TLS termination for SSL acceptors

// Ingress naming (Kubernetes)  
Ingress: "<service-name>-ing"                   // Ingress per service
// Convention: Requires ingress controller with SSL passthrough support
```
pkg/resources/services/service.go: Services are pkg/resources/services/service.go  
pkg/resources/routes/route.go: Routes pkg/resources/routes/route.go when pkg/resources/routes/route.go  
pkg/resources/ingresses/ingress.go: Ingresses are pkg/resources/ingresses/ingress.go

## Configuration Precedence Conventions

The operator controllers/activemqartemis_reconciler.go for configuration sources. This section demonstrates how the operator consistently prioritizes **explicit user configuration** over **environment-based overrides** over **defaults** across different aspects of the system.

Two key examples illustrate this precedence philosophy:

**1. Image Selection Precedence** - How the operator chooses which container image to use for the broker (infrastructure concern):

```go
// Precedence order (highest to lowest):
1. Explicit CR field values (e.g., spec.deploymentPlan.image)
2. Environment variable overrides (e.g., DEFAULT_BROKER_KUBE_IMAGE)  
3. Architecture-specific related images (RELATED_IMAGE_*_<arch>)
4. Generic related images (RELATED_IMAGE_*)
5. Built-in defaults (LatestKubeImage, LatestInitImage)
```

**2. Broker Properties Precedence** - How the operator merges broker configuration properties (application concern):

```go
// Precedence order (highest to lowest):
1. CR spec.brokerProperties (applied first)
2. External -bp secrets (alphabetical by secret name)
3. Files within -bp secrets (alphabetical by key name)
4. Operator-injected properties (restricted mode, metrics, etc.)
```
pkg/utils/common/common.go: Images are pkg/utils/common/common.go in `DetermineImageToUse()`  
controllers/activemqartemis_reconciler.go: Properties are controllers/activemqartemis_reconciler.go during JVM configuration

## Zero-Configuration Deployment Convention

A minimal CR demonstrates the convention over configuration philosophy:

```yaml
# Minimal working configuration - everything else uses conventions
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: my-broker
spec: {}  # Everything else is defaulted!

# This creates:
# - 1 broker pod (DefaultDeploymentSize)
# - All standard ports (61616, 8161, 8778, 7800)
# - Headless and ping services
# - Latest stable broker image
# - Ephemeral storage
# - Standard labels and naming
# - CORE protocol enabled for clustering
```

## Suffix-Based Magic Behavior

The operator uses suffix-based conventions for automatic behavior:

### **ExtraMount Secret Suffixes**
- **`-bp`**: controllers/activemqartemis_reconciler.go when secrets are controllers/activemqartemis_reconciler.go
- **`-jaas-config`**: controllers/activemqartemis_controller.go (must be Secret, not ConfigMap) - controllers/activemqartemis_controller.go
- **`-logging-config`**: controllers/activemqartemis_controller.go (must be ConfigMap) - controllers/activemqartemis_controller.go

### **File Extension Processing**
- **`.properties`**: controllers/activemqartemis_reconciler.go (default handling)
- **`.json`**: controllers/activemqartemis_reconciler.go (broker parses internally)
- **`.config`**: controllers/activemqartemis_reconciler.go files

## Mount Path Conventions

```bash
# Base paths (defined as constants)
ConfigMaps: "/amq/extra/configmaps/<name>/"  # cfgMapPathBase at line 79
Secrets: "/amq/extra/secrets/<name>/"        # secretPathBase at line 80

# Specific paths
BrokerProperties: "/amq/extra/secrets/<name>/broker.properties"
JaasConfig: "/amq/extra/secrets/<name>/login.config"           # JaasConfigKey
LoggingConfig: "/amq/extra/configmaps/<name>/logging.properties" # LoggingConfigKey
```
controllers/activemqartemis_reconciler.go: Base paths are controllers/activemqartemis_reconciler.go for all configuration files

## Environment Variable Patterns

The operator recognizes different environment variables based on deployment mode and operational context. All environment variables default to empty/unset unless explicitly configured.

### **JVM Configuration Variables (Mode-Dependent)**

The operator controllers/activemqartemis_reconciler.go based on deployment mode:

**Legacy Mode** (`restricted: false`):
- **JAVA_ARGS_APPEND**: controllers/activemqartemis_reconciler.go in controllers/activemqartemis_reconciler.go
- **JAVA_OPTS**: controllers/activemqartemis_reconciler.go for JVM configuration
- **DEBUG_ARGS**: Debug configuration options

**Restricted Mode** (`restricted: true`):
- **JDK_JAVA_OPTIONS**: controllers/activemqartemis_reconciler.go (preferred) controllers/activemqartemis_reconciler.go
- **STATEFUL_SET_ORDINAL**: controllers/activemqartemis_reconciler.go for controllers/activemqartemis_reconciler.go

### **Operator Behavior Variables**

These environment variables control operator-level behavior:

- **OPERATOR_WATCH_NAMESPACE**: Limits the operator to watch specific namespace(s). Empty = all namespaces (cluster-wide)
- **OPERATOR_NAMESPACE**: The namespace where the operator itself is deployed
- **OPERATOR_OPENSHIFT**: Override automatic OpenShift detection (`"true"` or `"false"`)
- **OPERATOR_LOG_LEVEL**: Set operator logging level (`debug`, `info`, `warn`, `error`)

### **Image Selection Variables**

See [Configuration Precedence Conventions](#configuration-precedence-conventions) for the complete list of image-related environment variables (`DEFAULT_BROKER_VERSION`, `DEFAULT_BROKER_KUBE_IMAGE`, `RELATED_IMAGE_*`, etc.)

## Constants and Magic Values

Important constants that controllers/activemqartemis_reconciler.go:

```go
// Suffixes (lines 75-77)
brokerPropsSuffix = "-bp"
jaasConfigSuffix = "-jaas-config"
loggingConfigSuffix = "-logging-config"

// Prefixes and Separators (lines 82-84)
OrdinalPrefix = "broker-"
OrdinalPrefixSep = "."
UncheckedPrefix = "_"

// File Extensions (lines 85-87)
PropertiesSuffix = ".properties"
JsonSuffix = ".json"
BrokerPropertiesName = "broker.properties"

// Special Values (line 94)
RemoveKeySpecialValue = "-"  // Used to remove/disable features
```
controllers/activemqartemis_reconciler.go: These constants are controllers/activemqartemis_reconciler.go to controllers/activemqartemis_reconciler.go

## Regex Patterns

### **Ordinal Property Parsing**
```go
// Matches: broker-0.property=value, broker-123.another=value
Pattern: ^(broker-[0-9]+)\.(.*)$
```
controllers/activemqartemis_reconciler.go: Regex is controllers/activemqartemis_reconciler.go ordinal-specific properties

### **JAAS Config Validation**
```go
// Complex regex for JAAS syntax validation (customizable via env var)
JAAS_CONFIG_SYNTAX_MATCH_REGEX: ^(?:(\s*|(?://.*)|(?s:/\*.*\*/))*\S+\s*{...
```
pkg/utils/common/common.go: Regex pkg/utils/common/common.go and is pkg/utils/common/common.go

## Hash-Based Resource Management

The operator controllers/activemqartemis_reconciler.go for efficient change detection:

```go
// Legacy immutable ConfigMaps with hash in name
ConfigMapName: "<cr-name>-props-<adler32-hash>"

// Modern mutable Secrets with fixed name  
SecretName: "<cr-name>-props"
```
controllers/activemqartemis_reconciler.go: Checksums are controllers/activemqartemis_reconciler.go to controllers/activemqartemis_reconciler.go

## Volume Mount Ordering

Properties are controllers/activemqartemis_reconciler.go:
1. controllers/activemqartemis_reconciler.go (global first, then ordinal-specific)
2. controllers/activemqartemis_reconciler.go (controllers/activemqartemis_reconciler.go)
3. controllers/activemqartemis_reconciler.go: controllers/activemqartemis_reconciler.go

## Default Values Reference

> **Complete Default Values Documentation**: For a comprehensive reference of all default values (deployment sizing, container images, ports, paths, timeouts, JAAS configuration, RBAC, probes, etc.), see the **[Operator Defaults Reference](operator_defaults_reference.md)**.

The operator follows a "convention over configuration" philosophy, providing sensible defaults for:
- Single-broker deployment by default
- NIO journal type
- Message migration enabled for safe scale-down
- Management RBAC enabled
- Jolokia agent enabled
- Standard protocol ports in legacy mode
- No exposed ports in restricted mode (explicit configuration required)
- 30-second reconciliation period
- 5-second liveness probe delay

All defaults can be overridden through CR specification, environment variables, or operator configuration according to [configuration precedence conventions](#configuration-precedence-conventions).

## Broker Version Dependencies

- **Ordinal-specific properties**: Requires broker >= 2.27.1
- **Directory-based property loading**: Requires broker >= 2.27.1
- **JSON format support**: Available in all supported versions

## Certificate Management Conventions

The operator integrates with `cert-manager` for automated certificate lifecycle management:

**Certificate Names**: See [Certificate Management Defaults](operator_defaults_reference.md#certificate-management-defaults) for default secret names

**Key Conventions**:
- All certificates use the same CA for trust relationships
- Certificates are auto-renewed by cert-manager before expiration
- mTLS is **required** in restricted mode
- mTLS is **optional** in legacy mode (can use plain-text acceptors)
- Certificates are mounted as secrets in the broker pods

**Source**: pkg/utils/common/common.go::pkg/utils/common/common.go#L69

## Annotation and Label Conventions

```go
// Control annotations
BlockReconcileAnnotation = "arkmq.org/block-reconcile"  // Pause reconciliation

// Standard Kubernetes labels
"statefulset.kubernetes.io/pod-name"  // Pod identification
"app.kubernetes.io/name"             // Application name
"app.kubernetes.io/instance"         // Instance identifier
"app.kubernetes.io/version"          // Version tracking
"app.kubernetes.io/component"        // Component type
"app.kubernetes.io/part-of"          // Application suite
"app.kubernetes.io/managed-by"       // Management tool

// Operator-specific labels (../../pkg/utils/selectors/label.go)
"application": "<cr-name>-app"        // Application grouping
"ActiveMQArtemis": "<cr-name>"        // Resource association
```

## Security Conventions

**JAAS Configuration**: See [JAAS Configuration Defaults](operator_defaults_reference.md#jaas-configuration-defaults) for complete JAAS defaults, including:
- Syntax validation regex (customizable via environment variable)
- Automatic JAAS generation in restricted mode
- User-provided JAAS in legacy mode

**Address Settings**: Default apply rule is `merge_all` for merging address setting configurations

**Key Security Conventions**:
- Restricted mode **requires** mTLS for all communication (broker, management, metrics)
- Legacy mode **allows** plain-text connections for development/testing
- Management RBAC always enabled by default
- Operator has full access to all broker management functions
- In restricted mode: no web console, no init container, certificate-based authentication only

**Source**: pkg/utils/common/common.go::pkg/utils/common/common.go#L60 (JAAS validation), controllers/activemqartemis_reconciler.go::controllers/activemqartemis_reconciler.go#L110 (apply rule)

