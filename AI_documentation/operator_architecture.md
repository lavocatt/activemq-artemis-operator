This document provides a comprehensive technical overview of the ActiveMQ Artemis Operator's architecture and implementation details, intended for developers who need to understand how the operator works internally.

**Table of Contents**

1.  [High-Level Architecture](#1-high-level-architecture)
    *   [Reconciler Interaction](#reconciler-interaction)
2.  [Reconciler Logic and Flow](#2-reconciler-logic-and-flow)
    *   [Main Reconciliation Flow](#main-reconciliation-flow)
    *   [Managed Resources](#managed-resources)
    *   [Acceptor and Connector Configuration](#acceptor-and-connector-configuration)
3.  [Reconciler State Machine](#3-reconciler-state-machine)
4.  [Deprecated Custom Resources](#4-deprecated-custom-resources)
5.  [Configuring Broker Properties](#5-configuring-broker-properties)
    *   [How it Works](#how-it-works)
    *   [Unit Tests Related to Broker Properties](#unit-tests-related-to-broker-properties)
6.  [Restricted (Locked-Down) Mode](#6-restricted-locked-down-mode)
    *   [Overview](#overview)
    *   [Implementation Details](#implementation-details)
    *   [Configuration Example](#configuration-example)
7.  [StatefulSet Management and Resource Reconciliation](#7-statefulset-management-and-resource-reconciliation)
    *   [StatefulSet Creation and Updates](#statefulset-creation-and-updates)
    *   [Resource Templates and Strategic Merge Patches](#resource-templates-and-strategic-merge-patches)
    *   [Message Migration on Scale-Down](#message-migration-on-scale-down)
8.  [Metrics Implementation](#8-metrics-implementation)
    *   [Metrics Plugin Selection Logic](#metrics-plugin-selection-logic)
    *   [Legacy Mode Implementation (Artemis Metrics Plugin)](#legacy-mode-implementation-artemis-metrics-plugin)
    *   [Restricted Mode Implementation (JMX Exporter)](#restricted-mode-implementation-jmx-exporter)
    *   [Service Discovery and Port Naming](#service-discovery-and-port-naming)
9.  [Version Management and Upgrade System](#9-version-management-and-upgrade-system)
    *   [Version Resolution Algorithm](#version-resolution-algorithm)
    *   [Image Selection Precedence](#image-selection-precedence)
    *   [Upgrade Policy Management](#upgrade-policy-management)
    *   [Version Validation Workflow](#version-validation-workflow)
10. [Validation Architecture](#10-validation-architecture)
    *   [Validation Chain Pattern](#validation-chain-pattern)
    *   [Individual Validation Functions](#individual-validation-functions)
    *   [Validation Error Handling](#validation-error-handling)
11. [Multi-Namespace Deployment](#11-multi-namespace-deployment)
    *   [Namespace Watching Modes](#namespace-watching-modes)
    *   [WATCH_NAMESPACE Configuration](#watch_namespace-configuration)
    *   [Manager Configuration](#manager-configuration)
12. [High Availability and Resilience](#12-high-availability-and-resilience)
    *   [PodDisruptionBudget Configuration](#poddisruptionbudget-configuration)
    *   [Affinity and Anti-Affinity](#affinity-and-anti-affinity)
    *   [Tolerations and Node Selection](#tolerations-and-node-selection)
    *   [TopologySpreadConstraints](#topologyspreadconstraints)
13. [Probe Configuration](#13-probe-configuration)
    *   [Default Probe Configurations](#default-probe-configurations)
    *   [Probe Override Mechanism](#probe-override-mechanism)
    *   [Probe Types and Use Cases](#probe-types-and-use-cases)
14. [Logging Configuration](#14-logging-configuration)
    *   [Custom Logging Properties](#custom-logging-properties)
    *   [Protected Environment Variables](#protected-environment-variables)
    *   [Logging Validation](#logging-validation)
15. [Certificate Management](#15-certificate-management)
    *   [cert-manager Integration](#cert-manager-integration)
    *   [Certificate Lifecycle](#certificate-lifecycle)
    *   [TLS Configuration Patterns](#tls-configuration-patterns)
16. [Operational Controls](#16-operational-controls)
    *   [Block Reconcile Annotation](#block-reconcile-annotation)
    *   [Resource Tracking and Adoption](#resource-tracking-and-adoption)
    *   [Recovery Patterns](#recovery-patterns)
17. [Console and Web UI Architecture](#17-console-and-web-ui-architecture)
    *   [Console Exposure Mechanisms](#console-exposure-mechanisms)
    *   [Jolokia Integration and Metrics Ports](#jolokia-integration-and-metrics-ports)
    *   [Console SSL Configuration](#console-ssl-configuration)
18. [Networking and Exposure Architecture](#18-networking-and-exposure-architecture)
    *   [Acceptor Architecture](#acceptor-architecture)
    *   [Connector Architecture](#connector-architecture)
    *   [Expose Mode Selection](#expose-mode-selection)
19. [Credential and Secret Management](#19-credential-and-secret-management)
    *   [User Credential Generation](#user-credential-generation)
    *   [Console Secret Lifecycle](#console-secret-lifecycle)
    *   [Secret Persistence and Stability](#secret-persistence-and-stability)
20. [Resource Metadata Management](#20-resource-metadata-management)
    *   [Custom Annotations Propagation](#custom-annotations-propagation)
    *   [Custom Labels Management](#custom-labels-management)
    *   [Operator-Managed Labels](#operator-managed-labels)
21. [Storage and Persistence Architecture](#21-storage-and-persistence-architecture)
    *   [PVC Creation and Management](#pvc-creation-and-management)
    *   [PVC Retention and Persistence](#pvc-retention-and-persistence)
    *   [PVC Adoption for Upgrades](#pvc-adoption-for-upgrades)
    *   [Dynamic Persistence Toggle](#dynamic-persistence-toggle)
22. [Error Handling and Status Management](#22-error-handling-and-status-management)
    *   [Error Detection and Reporting](#error-detection-and-reporting)
    *   [Status Condition Management](#status-condition-management)
    *   [Pod Status Tracking](#pod-status-tracking)

## 1. High-Level Architecture

The operator is composed of a core controller that works to manage ActiveMQ Artemis clusters. The controllers/activemqartemis_controller.go::ActiveMQArtemisReconciler is the core component, responsible for the main broker deployment.

The operator's entry point is main.go::main.go, which sets up the controller manager and registers all reconcilers.

### Reconciler Interaction

The diagram below shows the primary relationship between user actions and the controller.

```mermaid
graph TD
    subgraph "User Actions"
        UserArtemis[Apply ActiveMQArtemis CR]
    end

    subgraph "Operator Controller"
        ArtemisReconciler[ActiveMQArtemisReconciler]
    end
    
    subgraph "Kubernetes Cluster Resources"
        StatefulSet[StatefulSet]
        BrokerPods[Broker Pods]
    end

    UserArtemis -- triggers --> ArtemisReconciler

    ArtemisReconciler -- "creates/manages" --> StatefulSet
    StatefulSet -- "creates" --> BrokerPods

    style ArtemisReconciler fill:#f9f,stroke:#333,stroke-width:2px
```

## 2. Reconciler Logic and Flow

The controller has a reconciliation loop that is triggered by changes to the `ActiveMQArtemis` Custom Resource. The following diagrams illustrate the internal logic of the loop.

### Main Reconciliation Flow

The controllers/activemqartemis_reconciler.go is the main entry point for the reconciliation loop. It orchestrates a series of steps to converge the actual state of the cluster with the desired state defined in the CR. The high-level flow is as follows:

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - 200+ scenarios testing the complete reconciliation lifecycle

*   **Observe**: The reconciler first observes the current state of the cluster by retrieving all deployed resources it manages (e.g., `StatefulSet`, `Services`, `Secrets`).
*   **Process CR**: It then processes the `ActiveMQArtemis` CR to determine the desired state, including:
    *   The `StatefulSet` definition (controllers/activemqartemis_reconciler.go::ProcessStatefulSet).
    *   The deployment plan (image, size, etc.) (controllers/activemqartemis_reconciler.go::ProcessDeploymentPlan).
    *   Credentials (controllers/activemqartemis_reconciler.go::ProcessCredentials).
    *   Network acceptors and connectors (controllers/activemqartemis_reconciler.go::ProcessAcceptorsAndConnectors).
    *   The web console configuration (controllers/activemqartemis_reconciler.go::ProcessConsole).
*   **Track Changes**: The desired state is tracked via controllers/activemqartemis_reconciler.go::trackDesired() and secret checksums are computed in controllers/activemqartemis_reconciler.go::trackSecretCheckSumInEnvVar() to trigger rolling updates when changes are detected.
*   **Apply**: Finally, the reconciler applies the changes to the cluster by creating, updating, or deleting resources to match the desired state (controllers/activemqartemis_reconciler.go::ProcessResources).

### Managed Resources

The reconciler creates and manages a variety of Kubernetes resources to build the cluster. Resource creation is handled by dedicated builders in pkg/resources/::../../pkg/resources/ and orchestrated by controllers/activemqartemis_reconciler.go::ProcessResources().

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - Comprehensive resource creation and lifecycle testing

```mermaid
graph TD
    subgraph "Custom Resource"
        CR[brokerv1beta1.ActiveMQArtemis]
    end

    subgraph "Core Workload"
        SS[appsv1.StatefulSet]
        PDB[policyv1.PodDisruptionBudget]
        PVC[corev1.PersistentVolumeClaim]
    end

    subgraph "Configuration"
        Secrets[corev1.Secret]
        CM[corev1.ConfigMap]
    end

    subgraph "Networking"
        HeadlessSvc[corev1.Service Headless]
        PingSvc[corev1.Service Ping]
        AcceptorSvc[corev1.Service Acceptor]
        Route[routev1.Route]
        Ingress[netv1.Ingress]
    end

    CR -- defines --> SS; CR -- defines --> PDB; SS -- uses --> PVC
    CR -- defines --> Secrets; CR -- defines --> CM; SS -- mounts --> Secrets
    SS -- mounts --> CM; SS -- controlled by --> HeadlessSvc
    SS -- controlled by --> PingSvc; AcceptorSvc -- targets --> SS
    Route -- exposes --> AcceptorSvc; Ingress -- exposes --> AcceptorSvc
    CR -- defines --> AcceptorSvc; CR -- defines --> Route; CR -- defines --> Ingress
```

**Resource Creation Implementations**:
- **StatefulSet**: pkg/resources/statefulsets/statefulset.go::../../pkg/resources/statefulsets/statefulset.go → `MakeStatefulSet()`
- **Services**: pkg/resources/services/service.go::../../pkg/resources/services/service.go → `NewHeadlessServiceForCR2()`, `NewServiceDefinitionForCR()` 
- **Routes**: pkg/resources/routes/route.go::../../pkg/resources/routes/route.go → `NewRouteDefinitionForCR()`
- **Ingress**: pkg/resources/ingresses/ingress.go::../../pkg/resources/ingresses/ingress.go → `NewIngressForCRWithSSL()`
- **Secrets**: pkg/resources/secrets/secret.go::../../pkg/resources/secrets/secret.go → `MakeSecret()`, `NewSecret()`
- **ConfigMaps**: pkg/resources/configmaps/::../../pkg/resources/configmaps/ → ConfigMap builders
- **Pods**: pkg/resources/pods/pod.go::../../pkg/resources/pods/pod.go → `MakePodTemplateSpec()`

### Acceptor and Connector Configuration

This process translates CR configurations into Kubernetes networking resources. The reconciler generates acceptor and connector configuration strings via controllers/activemqartemis_reconciler.go::generateAcceptorsString() and controllers/activemqartemis_reconciler.go::generateConnectorsString(), creates corresponding Kubernetes Services, and optionally exposes them via Routes or Ingresses based on the CR specification.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - TLS secret reuse, networking, acceptor/connector configuration scenarios

## 3. Reconciler State Machine

The controller functions as a state machine driven by cluster events. The following diagram illustrates the states and transitions for the controllers/activemqartemis_controller.go::ActiveMQArtemisReconciler. State transitions are managed through status conditions in controllers/activemqartemis_controller.go::ProcessStatus().

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - State machine transitions and error handling scenarios

```mermaid
stateDiagram-v2
    [*] --> Pending: CR Created
    Pending --> Creating: Initial reconcile
    Creating --> PodsStarting: Resources Provisioned
    Creating --> Error: Deployment failed
    PodsStarting --> Ready: Pods Ready
    Ready --> Updating: CR Spec Changed
    Ready --> ScalingUp: Broker count increased
    Ready --> ScalingDown: Broker count decreased
    Ready --> [*]: CR Deleted
    Updating --> Ready: Update Complete
    Updating --> Error: Update Failed
    ScalingUp --> PodsStarting: Replicas Increased
    ScalingDown --> DrainingPods: Draining Pods
    DrainingPods --> DecreasingReplicas: Drain Complete
    DecreasingReplicas --> Ready: Scale-down Complete
    Error --> Pending: Re-reconcile triggered
```

## 4. Deprecated Custom Resources

The operator is moving towards a model where all broker configuration is done via `brokerProperties` on the main `ActiveMQArtemis` CR. As a result, the following custom resources are considered deprecated and should not be used for new deployments:

**🧪 Tested in**: 
- controllers/activemqartemisaddress_controller_test.go::activemqartemisaddress_controller_test.go - Legacy address controller testing
- controllers/activemqartemissecurity_controller_test.go::activemqartemissecurity_controller_test.go - Legacy security controller testing
- controllers/activemqartemisscaledown_controller_test.go::activemqartemisscaledown_controller_test.go - Legacy scaledown controller testing

*   **`ActiveMQArtemisAddress`**: Address and queue configuration should now be done using the `addressConfigurations` properties (see [Section 5: Broker Properties Examples](#unit-tests-related-to-broker-properties)). API definition: api/v1beta1/activemqartemisaddress_types.go::../../api/v1beta1/activemqartemisaddress_types.go, Controller implementation: controllers/activemqartemisaddress_controller.go::activemqartemisaddress_controller.go
*   **`ActiveMQArtemisSecurity`**: Security domains and permissions should now be configured using the `securityRoles` properties (see [Section 5: Broker Properties Examples](#unit-tests-related-to-broker-properties)). API definition: api/v1beta1/activemqartemissecurity_types.go::../../api/v1beta1/activemqartemissecurity_types.go, Controller implementation: controllers/activemqartemissecurity_controller.go::activemqartemissecurity_controller.go
*   **`ActiveMQArtemisScaledown`**: Message draining and migration on scale-down is now controlled by the `spec.deploymentPlan.messageMigration` flag on the `ActiveMQArtemis` CR (see [Section 7: Message Migration](#message-migration-on-scale-down)). API definition: api/v1beta1/activemqartemisscaledown_types.go::../../api/v1beta1/activemqartemisscaledown_types.go, Controller implementation: controllers/activemqartemisscaledown_controller.go::activemqartemisscaledown_controller.go

While the controllers for these CRs still exist to support legacy deployments, they will be removed in a future release.

## 5. Configuring Broker Properties

> [!Note]
> Configuring the broker via `brokerProperties` is the recommended and most flexible approach. It allows direct access to the broker's internal configuration bean and should be preferred over legacy mechanisms like `addressSettings` or the deprecated CRs.

The operator provides a mechanism to directly configure the internal settings of the ActiveMQ Artemis broker through the api/v1beta1/activemqartemis_types.go (defined as `[]string`) on the api/v1beta1/activemqartemis_types.go. This allows you to fine-tune the broker's behavior, override defaults, and configure features that are not explicitly exposed as attributes in the CRD.

**🧪 Tested in**: 
- controllers/activemqartemis_address_broker_properties_test.go::activemqartemis_address_broker_properties_test.go - Address and queue configuration via properties
- controllers/activemqartemissecurity_broker_properties_test.go::activemqartemissecurity_broker_properties_test.go - Security configuration via properties  
- controllers/activemqartemis_reconciler_test.go::activemqartemis_reconciler_test.go - Unit tests for property parsing functions

For a complete list of available properties, refer to the official [ActiveMQ Artemis configuration documentation](https://activemq.apache.org/components/artemis/documentation/latest/configuration-index.html#broker-properties). The operator processes these properties through controllers/activemqartemis_reconciler.go::BrokerPropertiesData() and validates them in controllers/activemqartemis_reconciler.go::AssertBrokerPropertiesStatus().

### How it Works

From a developer's perspective, the broker properties configuration flow works as follows:

#### 1. **Property Processing (Creating Properties from CR)**

When the operator controllers/activemqartemis_reconciler.go from the CR during reconciliation, it controllers/activemqartemis_reconciler.go with controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go (using regex `^(broker-[0-9]+)\.(.*)$`) if necessary to create per-broker files.

The `brokerProperties` field is an array of strings, where each string is a `key=value` pair:

```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: ex-aao
spec:
  brokerProperties:
    - "globalMaxSize=512m"
    - "address-memory-usage-full-policy=FAIL"
```

#### 1b. **Property Status Validation (Verifying Deployed Properties)**

After properties are deployed, the operator validates they were correctly applied and controllers/activemqartemis_reconciler.go. This validation flow computes checksums to detect if deployed properties match expectations:

1. controllers/activemqartemis_reconciler.go::AssertBrokerPropertiesStatus initiates validation
2. controllers/activemqartemis_reconciler.go::getSecretProjection retrieves the deployed secret
3. controllers/activemqartemis_reconciler.go::newProjectionFromByteValues processes the data
4. controllers/activemqartemis_reconciler.go::alder32FromData computes checksums for status reporting
5. controllers/activemqartemis_reconciler.go::KeyValuePairs normalizes properties (removes comments/whitespace)
6. controllers/activemqartemis_reconciler.go::appendNonEmpty performs controllers/activemqartemis_reconciler.go for `\ `, `\:`, `\=`, `\"` to ensure checksum consistency

**Note on Special Characters**: Properties are stored in standard Java properties file format. If your keys or values need to include special characters like spaces (` `), colons (`:`), or equals signs (`=`), you must escape them with a backslash (`\`). For example, FQQN with `::` separator requires escaping: `"securityRoles.\"TOPIC\\:\\:FOR_TOM\".toms.send=true"`.

This status validation is separate from the rolling update trigger mechanism described below.

#### 2. **Secret Creation for Broker Properties**

The operator stores broker properties in a Kubernetes resource, with automatic upgrade from legacy ConfigMaps to modern Secrets:

- **controllers/activemqartemis_reconciler.go**: First checks if an controllers/activemqartemis_reconciler.go exists (e.g., `<cr-name>-props-<hash>`) - if found, controllers/activemqartemis_reconciler.go for backward compatibility
- **controllers/activemqartemis_reconciler.go**: Otherwise, creates/updates a controllers/activemqartemis_reconciler.go `<cr-name>-props` containing controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: For modern Secrets, a separate mechanism computes controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go on the StatefulSet pod template - when annotations change, Kubernetes automatically triggers a rolling update (this is different from the status validation checksums above)

#### 3. **Ordinal-Specific File Organization**

When ordinal-specific properties are detected (properties prefixed with `broker-N.` where `N` is the pod ordinal - see [ordinal prefix convention](operator_conventions.md#constants-and-magic-values)), the operator creates separate property files for each targeted ordinal within the secret:

```
Secret Data Structure:
├── broker.properties           (global properties)
├── broker-0.broker.properties  (ordinal 0 specific)
├── broker-1.broker.properties  (ordinal 1 specific)
└── broker-N.broker.properties  (ordinal N specific)
```

Example CR with ordinal-specific configuration:

```yaml
spec:
  brokerProperties:
    # Global property - applies to all brokers
    - "globalMaxSize=512m"
    # Ordinal-specific - applies ONLY to broker pod ordinal 0
    - "broker-0.management-address=prod.management.0"
    # Ordinal-specific - applies ONLY to broker pod ordinal 1
    - "broker-1.management-address=prod.management.1"
```

Properties can also be mixed, where ordinal-specific values override global settings for that specific broker pod.

#### 4. **Volume Mount Architecture**

Properties are controllers/activemqartemis_reconciler.go during pod template creation:

- **controllers/activemqartemis_reconciler.go**: `/amq/extra/secrets/<secret-name>/` using the `secretPathBase` constant
- **controllers/activemqartemis_reconciler.go**: `/amq/extra/secrets/<secret-name>/broker-N/broker.properties` created when controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: Additional paths at `/amq/extra/secrets/<bp-secret-name>/` when controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: Volumes and mounts are controllers/activemqartemis_reconciler.go to the pod template

#### 5. **JVM Configuration Generation**

The operator controllers/activemqartemis_reconciler.go to tell the broker where to find properties:

**Single Global Properties**:
```bash
-Dbroker.properties=/amq/extra/secrets/<name>/broker.properties
```

**Multiple Ordinal Properties** (requires broker >= 2.27.1):
```bash
-Dbroker.properties=/amq/extra/secrets/<name>/,/amq/extra/secrets/<name>/broker-${STATEFUL_SET_ORDINAL}/
```

**With External `-bp` Secrets**:
```bash
-Dbroker.properties=/amq/extra/secrets/<name>/,/amq/extra/secrets/<name>/broker-${STATEFUL_SET_ORDINAL}/,/amq/extra/secrets/<bp-secret>/,/amq/extra/secrets/<bp-secret>/broker-${STATEFUL_SET_ORDINAL}/
```

#### 6. **Format Support and Processing**

The operator supports both `.properties` and `.json` formats in `-bp` secrets:

- **Properties Format**: Standard Java properties files (handled by default for any key)
- **JSON Format**: Keys ending with `.json` are controllers/activemqartemis_reconciler.go (broker handles JSON parsing internally)
- **Alphabetical Loading**: Properties from secrets are controllers/activemqartemis_reconciler.go by key name when controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: CR `brokerProperties` → `-bp` secrets (alphabetically) - enforced by controllers/activemqartemis_reconciler.go, then controllers/activemqartemis_reconciler.go to the `-Dbroker.properties` argument

**External `-bp` Secrets**: To organize properties externally or source them from a different management system, create a secret with a `-bp` suffix (for **b**roker **p**roperties - see [suffix-based magic behavior](operator_conventions.md#suffix-based-magic-behavior)) and controllers/activemqartemis_reconciler.go. The operator will automatically detect the suffix and include the secret's property files in the broker configuration path.

#### 7. **Status Tracking and Validation**

The operator controllers/activemqartemis_reconciler.go to ensure properties are properly applied:

- **controllers/activemqartemis_reconciler.go**: `BrokerPropertiesApplied` condition in CR status reflects whether controllers/activemqartemis_reconciler.go by comparing checksums
- **controllers/activemqartemis_reconciler.go**: Adler-32 hashes computed controllers/activemqartemis_reconciler.go to trigger updates
- **controllers/activemqartemis_reconciler.go**: Verification that properties secrets are controllers/activemqartemis_reconciler.go

This mechanism enables dynamic, declarative configuration while maintaining backward compatibility and supporting complex multi-broker scenarios.

### Unit Tests Related to Broker Properties

The broker properties functionality is extensively tested across multiple test files:

#### Property Processing and Parsing Tests
- **controllers/activemqartemis_reconciler_test.go::TestBrokerPropertiesData()**: Basic global property processing, validates transformation of CR properties into property files
- **controllers/activemqartemis_reconciler_test.go::TestBrokerPropertiesDataWithOrdinal()**: Ordinal-specific property separation, validates `broker-N.` prefix detection and file organization
- **controllers/activemqartemis_reconciler_test.go::TestBrokerPropertiesDataWithAndWithoutOrdinal()**: Mixed global and ordinal property handling, validates override behavior

#### Address and Queue Configuration Tests
- **controllers/activemqartemis_address_broker_properties_test.go::activemqartemis_address_broker_properties_test.go**: Address and queue configuration via `addressConfigurations` properties
- **controllers/activemqartemissecurity_broker_properties_test.go::activemqartemissecurity_broker_properties_test.go**: Security roles configuration via `securityRoles` properties
- **controllers/activemqartemis_pub_sub_scale_test.go::activemqartemis_pub_sub_scale_test.go**: Multicast address, FQQN escaping, and RBAC configuration

#### Integration and End-to-End Tests
- **controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go**: Address settings (dead letter queues, expiry), acceptor configuration, and broker deployment scenarios
- **controllers/activemqartemis_work_queue_test.go::activemqartemis_work_queue_test.go**: Work queue patterns using broker properties
- **controllers/activemqartemis_reconciler_test.go::activemqartemis_reconciler_test.go**: Unit tests for property parsing functions and reconciliation logic

These tests provide comprehensive coverage of property processing, validation, ordinal-specific configuration, external secret handling, escape sequences, and integration with broker deployment.

## 6. Restricted (Locked-Down) Mode

### Overview

For production and security-sensitive environments, the operator provides a "restricted" or "locked-down" mode. This mode controllers/activemqartemis_reconciler.go by enforcing comprehensive security configuration including mTLS, JAAS authentication, and RBAC. It is enabled by setting `spec.restricted: true` in the `ActiveMQArtemis` custom resource.

**🧪 Tested in**: controllers/controll_plane_test.go::controll_plane_test.go - Restricted RBAC and mTLS configuration scenarios

Key characteristics of restricted mode include:

*   **controllers/activemqartemis_reconciler.go**: The broker controllers/activemqartemis_reconciler.go, reducing its attack surface.
*   **controllers/activemqartemis_reconciler.go**: The Jetty-based web console is controllers/activemqartemis_reconciler.go. All management is done via the Jolokia agent.
*   **controllers/activemqartemis_reconciler.go**: The broker is controllers/activemqartemis_reconciler.go, eliminating XML parsing.
*   **controllers/activemqartemis_reconciler.go**: All management interfaces (Jolokia, Prometheus) are controllers/activemqartemis_reconciler.go. This requires `cert-manager` to be installed in the cluster.
*   **controllers/activemqartemis_reconciler.go**: Fine-grained Role-Based Access Control policies are controllers/activemqartemis_reconciler.go, with separate roles for operator status checks, metrics collection, and administrative access.

This mode is the recommended approach for new deployments.

### Implementation Details

When `restricted: true` is set, the controllers/activemqartemis_reconciler.go including:

1. **controllers/activemqartemis_reconciler.go**: Configures the controllers/activemqartemis_reconciler.go, enabling Java to work with PEM-formatted certificates from `cert-manager` without conversion to JKS format.

2. **controllers/activemqartemis_reconciler.go**: Sets up controllers/activemqartemis_reconciler.go, which validates client certificates against user and role mappings stored in broker properties files.

3. **controllers/activemqartemis_reconciler.go**: Maps certificate subjects to roles - the controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go. These mappings are stored in controllers/activemqartemis_reconciler.go.

4. **controllers/activemqartemis_reconciler.go**: Core broker settings including controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go, stored in controllers/activemqartemis_reconciler.go::aa_restricted.properties.

5. **controllers/activemqartemis_reconciler.go**: Fine-grained access control rules stored in controllers/activemqartemis_reconciler.go::aa_rbac.properties, granting the controllers/activemqartemis_reconciler.go and the controllers/activemqartemis_reconciler.go.

6. **controllers/activemqartemis_reconciler.go**: Configures the controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go with controllers/activemqartemis_reconciler.go, stored in controllers/activemqartemis_reconciler.go::_jolokia.config and controllers/activemqartemis_reconciler.go. The JAAS configuration references controllers/activemqartemis_reconciler.go: `hawtio` (for controllers/activemqartemis_reconciler.go), `operator` (for controllers/activemqartemis_reconciler.go), and `probe` (for controllers/activemqartemis_reconciler.go). These users are mapped to controllers/activemqartemis_reconciler.go: `status` (assigned to controllers/activemqartemis_reconciler.go), `metrics` (assigned to controllers/activemqartemis_reconciler.go), and `hawtio` (assigned to controllers/activemqartemis_reconciler.go).

7. **controllers/activemqartemis_reconciler.go**: Configures the controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go, stored in controllers/activemqartemis_reconciler.go::_prometheus_exporter.yaml and controllers/activemqartemis_reconciler.go.

8. **controllers/activemqartemis_reconciler.go**: Hardens the JVM with controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go.

All these security configurations are controllers/activemqartemis_reconciler.go, then controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go. The controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go to provide the necessary TLS credentials.

### Configuration Example

This example shows how to deploy a minimal broker in restricted mode. It assumes `cert-manager` is installed and configured with a `ClusterIssuer`.

```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: artemis-restricted
  namespace: my-namespace
spec:
  # Enable restricted mode
  restricted: true
  
  deploymentPlan:
    size: 1
    image: placeholder # Add your broker image here
  
  # Example broker property
  brokerProperties:
    - "messageCounterSamplePeriod=500"
    
  # The operator will automatically configure the necessary mTLS certs.
  # This example assumes default cert names and a cert-manager issuer.
  # You would typically have cert-manager create these secrets for you.
```

### Control Plane Configuration Override

**Code location**: `controllers/activemqartemis_reconciler.go` lines ~2047-2068

The control plane refers to the secure communication channels between the operator, operands (broker pods), and metrics collection systems. In restricted mode, the operator automatically generates default certificate-based authentication with predefined user and role mappings.

However, you can override these defaults by creating an optional secret named `<cr-name>-control-plane-override`. This allows you to:
- Add additional authorized clients (e.g., custom monitoring tools)
- Use custom certificate subjects that don't match the defaults
- Implement organization-specific RBAC policies

**Allowed override keys:**
- `_cert-users`: Maps certificate subjects to usernames (Java properties format with regex patterns)
- `_cert-roles`: Maps usernames to roles (Java properties format)
- `login.config`: JAAS login configuration
- `_security.config`: Java security properties

**Example override secret:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: artemis-restricted-control-plane-override
  namespace: my-namespace
type: Opaque
stringData:
  _cert-users: |
    # Default entries (must be included for proper functioning)
    hawtio=/CN = hawtio-online\.hawtio\.svc.*/
    operator=/.*activemq-artemis-operator.*/
    probe=/.*activemq-artemis-operand.*/
    # Custom entry for additional monitoring
    prometheus=/.*my-custom-prometheus.*/
  _cert-roles: |
    status=operator,probe
    metrics=operator,prometheus
    hawtio=hawtio
```

**Implementation notes:**
- The operator checks for the override secret after generating defaults
- If found, the specified keys completely replace the default values
- The override secret is automatically mounted to the broker pod
- No validation is performed on override values - users are responsible for correctness
- Changes require broker pod restart to take effect

**Testing**: See `controllers/controll_plane_test.go` test case "operator role access with control plane override" which validates that:
1. Operator can access metrics with default cert
2. Custom prometheus cert (added via override) can access metrics
3. Unauthorized certs are rejected

## 7. StatefulSet Management and Resource Reconciliation

> **Note**: For user-facing configuration examples of `deploymentPlan` settings, see the [operator help documentation](../help/operator.md). This section focuses on how the operator implements these features internally.

**🧪 Tested in**: 
- controllers/activemqartemis_rwm_pvc_ha_test.go::activemqartemis_rwm_pvc_ha_test.go - ReadWriteMany PVC high availability
- controllers/activemqartemis_jdbc_ha_test.go::activemqartemis_jdbc_ha_test.go - JDBC-based high availability
- controllers/activemqartemis_scale_zero_test.go::activemqartemis_scale_zero_test.go - Scale-to-zero scenarios
- controllers/activemqartemis_pub_sub_scale_test.go::activemqartemis_pub_sub_scale_test.go - Publish-subscribe scaling scenarios

### StatefulSet Creation and Updates

The reconciler controllers/activemqartemis_reconciler.go during reconciliation:

1. **controllers/activemqartemis_reconciler.go**: The operator constructs the `StatefulSet` spec based on CR fields using a controllers/activemqartemis_reconciler.go that translates CR attributes into pod templates, then controllers/activemqartemis_reconciler.go with all containers, volumes, and security settings.

2. **controllers/activemqartemis_reconciler.go**: Each broker pod in the `StatefulSet` receives an ordinal (0, 1, 2...). The operator controllers/activemqartemis_reconciler.go and uses it to controllers/activemqartemis_reconciler.go through `broker-N.` prefixed properties that are controllers/activemqartemis_reconciler.go.

3. **controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go based on:
   - controllers/activemqartemis_reconciler.go (`persistenceEnabled`) → controllers/activemqartemis_reconciler.go
   - controllers/activemqartemis_reconciler.go from `extraMounts` → controllers/activemqartemis_reconciler.go
   - controllers/activemqartemis_reconciler.go → controllers/activemqartemis_reconciler.go
   - controllers/activemqartemis_reconciler.go → controllers/activemqartemis_reconciler.go

4. **controllers/activemqartemis_reconciler.go**: The operator tracks changes that require pod restarts by:
   - controllers/activemqartemis_reconciler.go of mounted secrets/configmaps content
   - controllers/activemqartemis_reconciler.go in the pod template
   - Kubernetes controllers/activemqartemis_reconciler.go when the pod template changes

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - StatefulSet revision history limit configuration
- Ordinal-specific broker properties tests: controllers/activemqartemis_controller_test.go::ordinal broker properties, controllers/activemqartemis_controller_test.go::ordinal broker properties with other secret, controllers/activemqartemis_controller_test.go::invalid ordinal prefix broker properties, controllers/activemqartemis_controller_test.go::mod ordinal broker properties with error and update

### Resource Templates and Strategic Merge Patches

The `resourceTemplates` feature allows users to patch any operator-created resource:

- **controllers/activemqartemis_reconciler.go**: After generating a resource (e.g., `StatefulSet`), the reconciler controllers/activemqartemis_reconciler.go by controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go.
- **controllers/activemqartemis_reconciler.go**: Patches are controllers/activemqartemis_reconciler.go, allowing for predictable override behavior where controllers/activemqartemis_reconciler.go.
- **Validation**: The operator does not validate patch contents; invalid patches will cause reconciliation errors that surface in the CR status.

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - Template tests for Service and StatefulSet patches with annotations and strategic merge
- Tutorial: [`send_receive_ingress_pem.md`](../tutorials/send_receive_ingress_pem.md) - Real-world example using resourceTemplates to configure cert-manager annotations on Ingress resources

### Message Migration on Scale-Down

When controllers/activemqartemis_reconciler.go, the operator coordinates with the DrainController to safely scale down broker pods:

1. **controllers/activemqartemisscaledown_controller.go**: Before deleting a pod, the reconciler controllers/activemqartemisscaledown_controller.go and controllers/activemqartemisscaledown_controller.go, signaling the drain controller to begin message migration from the target broker.
2. **pkg/draincontroller/controller.go**: A separate controller pkg/draincontroller/controller.go to watch for StatefulSet changes and pkg/draincontroller/controller.go that use the broker's management API (via Jolokia) to move messages to other brokers in the cluster.
3. **Completion Check**: The drain controller pkg/draincontroller/controller.go, allowing the reconciler to proceed with the scale-down operation.
4. **controllers/activemqartemis_reconciler.go**: The PVC associated with the scaled-down pod is retained (not deleted) to allow for scale-up recovery or manual inspection.

**🧪 Tested in**: 
- controllers/activemqartemisscaledown_controller_test.go::activemqartemisscaledown_controller_test.go - Clustered broker scale-down with message migration
- controllers/activemqartemisscaledown_controller_test.go::activemqartemisscaledown_controller_test.go - Toleration verification during scale-down
- Related tests: controllers/activemqartemisaddress_controller_test.go::activemqartemisaddress_controller_test.go - Address removal during scale-down

## 8. Metrics Implementation

> **Note**: For user-facing configuration examples of metrics, see the [operator help documentation](../help/operator.md) and the [Prometheus tutorial](../tutorials/prometheus_locked_down.md). This section focuses on how the operator implements metrics support.

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - Console metrics port configuration
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - JVM metrics via broker properties

> **Related**: See [Section 6: Restricted Mode](#restricted-locked-down-mode) for security context and the [operator conventions documentation](operator_conventions.md) for port naming patterns.

### Metrics Plugin Selection Logic

The operator uses different metrics implementations based on the pkg/utils/common/common.go::restricted and controllers/activemqartemis_reconciler.go:

**Decision Tree**:
```
if enableMetricsPlugin == true:
    if restricted == true:
        → Use JMX Exporter (Java agent)
    else:
        → Use Artemis Metrics Plugin (JAR in classpath)
else:
    → No metrics exposed
```

### Legacy Mode Implementation (Artemis Metrics Plugin)

When `restricted: false` and `enableMetricsPlugin: true`:

1. **Classpath Modification**: The reconciler adds the Artemis Prometheus Metrics Plugin JAR to the broker's classpath via environment variables. The plugin is typically embedded in the init container or mounted from a ConfigMap.

2. **Plugin Initialization**: The broker automatically detects the plugin via Java's ServiceLoader mechanism and initializes it during startup.

3. **pkg/resources/serviceports/service_port.go**: The plugin registers a servlet at `/metrics` on the pkg/resources/serviceports/service_port.go (port `8161`). No additional port is opened.

4. **controllers/activemqartemis_reconciler.go**: The headless service is controllers/activemqartemis_reconciler.go for scraping. The pkg/resources/serviceports/service_port.go when pkg/resources/serviceports/service_port.go.

### Restricted Mode Implementation (JMX Exporter)

When `restricted: true` and `enableMetricsPlugin: true`:

1. **controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go to include `-javaagent:/opt/agents/prometheus.jar=$HOSTNAME:8888:/path/to/_prometheus_exporter.yaml`. The JAR is controllers/activemqartemis_reconciler.go via volume management.

2. **controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go (which JMX beans to expose, naming rules, etc.) is controllers/activemqartemis_reconciler.go as `_prometheus_exporter.yaml`. This configuration is controllers/activemqartemis_reconciler.go based on sensible defaults for Artemis, including controllers/activemqartemis_reconciler.go.

3. **controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go via the javaagent command line argument. In pkg/resources/serviceports/service_port.go, allowing the prometheus agent to listen on port 8888 within the pod without explicit service port exposure. This port is pkg/resources/serviceports/service_port.go.

4. **controllers/activemqartemis_reconciler.go**: The JMX exporter is controllers/activemqartemis_reconciler.go to use the same certificates as the broker. The exporter's HTTP server is controllers/activemqartemis_reconciler.go with controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go to require client authentication.

5. **controllers/activemqartemis_reconciler.go**: The mTLS authentication is controllers/activemqartemis_reconciler.go at the HTTP server level. Authorization (which clients can access) is controllers/activemqartemis_reconciler.go in the Java security configuration, where controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

### Service Discovery and Port Naming

In restricted mode, the operator does **not** automatically create a service for metrics. Users must manually create a Service resource to expose port 8888:

- **Manual Service Creation**: As shown in the [Prometheus locked-down tutorial](../tutorials/prometheus_locked_down.md#scrape-the-broker), users must [create a Service resource](../tutorials/prometheus_locked_down.md) with a port named `metrics` targeting port `8888` to expose the JMX exporter endpoint.
- **Port Naming**: Following Prometheus conventions (see [service discovery conventions](operator_conventions.md#service-discovery-and-networking-conventions)), the port should be named `metrics`, allowing `ServiceMonitor` resources to reference it by name rather than number.
- **Service and Pod Labels**: For label patterns and selector conventions, see [annotation and label conventions](operator_conventions.md#annotation-and-label-conventions).

---

## 9. Version Management and Upgrade System

> **Note**: For user-facing version configuration, see the [operator help documentation](../help/operator.md#version-management). This section focuses on the internal version management architecture.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - broker versions map (13 test scenarios covering version resolution, validation, and upgrade policies)

The operator implements a sophisticated version management system that resolves broker versions, validates image configurations, and manages upgrade policies. This system ensures that broker deployments use compatible images and follow controlled upgrade paths.

### Version Resolution Algorithm

The version resolution process determines which broker version to use based on the CR specification and operator defaults:

1. **pkg/utils/common/common.go**: The reconciler calls pkg/utils/common/common.go::ResolveImage() during controllers/activemqartemis_reconciler.go to determine the container image.

2. **pkg/utils/common/common.go**: If the user specifies explicit images (not `"placeholder"` and not empty), the operator pkg/utils/common/common.go without version resolution.

3. **version/version.go**: When no explicit image is provided, the operator version/version.go from version/version.go or falls back to version/version.go.

4. **pkg/utils/common/common.go**: The operator calls pkg/utils/common/common.go::DetermineImageToUse() which maps the broker version to specific container images using version/version.go.

**Version Sources** (in order of precedence):
1. api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.image - Explicit broker image
2. api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.initImage - Explicit init container image
3. `DEFAULT_BROKER_VERSION` environment variable - Operator-level default
4. version/version.go - Hardcoded fallback

### Image Selection Precedence

The operator enforces strict precedence rules for image selection:

**Decision Flow**:
```
1. Check spec.deploymentPlan.image
   ├─ If locked-down (not "placeholder", not "") → Use explicit image
   └─ If placeholder or empty → Proceed to version resolution

2. Check spec.version
   ├─ If specified → Resolve to image via version mapping
   └─ If empty → Use default version

3. Apply environment overrides
   ├─ Check DEFAULT_BROKER_KUBE_IMAGE env var
   └─ Fall back to LatestKubeImage constant
```

**Implementation**: controllers/activemqartemis_reconciler.go during pod template creation, where pkg/utils/common/common.go::common.ResolveImage() is called for both the broker and init container images.

### Upgrade Policy Management

The operator automatically determines which types of upgrades are allowed based on the version specification:

**pkg/utils/common/common.go**: During controllers/activemqartemis_controller.go, the reconciler calls pkg/utils/common/common.go::updateVersionStatus() to compute upgrade flags:

1. **pkg/utils/common/common.go**: If pkg/utils/common/common.go (user-specified images), pkg/utils/common/common.go to prevent the operator from changing user-controlled images.

2. **pkg/utils/common/common.go**: Always enabled unless using locked-down images, allowing security patches regardless of version specification.

3. **pkg/utils/common/common.go**: If `spec.version` is empty, pkg/utils/common/common.go. If specified, the operator pkg/utils/common/common.go:
   - **Major version only** (e.g., `"2"`): pkg/utils/common/common.go
   - **Major.minor** (e.g., `"2.28"`): pkg/utils/common/common.go
   - **Full version** (e.g., `"2.28.0"`): pkg/utils/common/common.go

**Upgrade Flags** (exposed in CR status):
```yaml
status:
  upgrade:
    majorUpdates: true/false    # Allow 2.x → 3.x
    minorUpdates: true/false    # Allow 2.28 → 2.29
    patchUpdates: true/false    # Allow 2.28.0 → 2.28.1
    securityUpdates: true/false # Allow security patches
```

### Version Validation Workflow

The operator validates version configurations before allowing deployment:

**controllers/activemqartemis_controller.go**: The controllers/activemqartemis_controller.go calls pkg/utils/common/common.go::common.ValidateBrokerImageVersion() to perform version validation.

**Validation Steps**:

1. **pkg/utils/common/common.go**: Attempts to pkg/utils/common/common.go via pkg/utils/common/common.go::ResolveBrokerVersionFromCR(). If resolution fails, pkg/utils/common/common.go.

2. **pkg/utils/common/common.go**: If using locked-down images, ensures pkg/utils/common/common.go, preventing mixed configurations. Partial specification pkg/utils/common/common.go.

3. **pkg/utils/common/common.go**: When explicit version is specified, verifies it's pkg/utils/common/common.go via version/version.go::version.IsSupportedActiveMQArtemisVersion().

4. **controllers/activemqartemis_reconciler.go**: After deployment, the reconciler controllers/activemqartemis_reconciler.go to verify the controllers/activemqartemis_reconciler.go, catching configuration or image mismatches.

**Validation Conditions** (set on CR status):
- `ValidConditionInvalidVersionReason`: Version doesn't resolve to supported broker version
- `ValidConditionUnknownReason`: Mixed locked-down/placeholder images, or unsupported version with explicit images
- `BrokerPropertiesApplied`: Version mismatch detected after deployment

**Error Example**:
```yaml
status:
  conditions:
  - type: Valid
    status: "False"
    reason: ValidConditionInvalidVersionReason
    message: ".Spec.Version does not resolve to a supported broker version, reason: version 1.2.3 not found"
```

---

## 10. Validation Architecture

> **Note**: For user-facing validation errors and troubleshooting, see the [operator help documentation](../help/operator.md#troubleshooting). This section focuses on the internal validation system architecture.

**🧪 Tested in**: controllers/activemqartemis_controller_unit_test.go::activemqartemis_controller_unit_test.go - 6 validation unit tests, plus 2 scenarios in main controller test

The operator implements a comprehensive validation system that prevents invalid configurations from being deployed. The validation architecture follows a chain-of-responsibility pattern, where each validator checks specific aspects of the CR configuration.

### Validation Chain Pattern

The validation system is structured as a sequential chain of validation functions, each responsible for specific configuration aspects:

**controllers/activemqartemis_controller.go**: The controllers/activemqartemis_controller.go in the reconciler controllers/activemqartemis_controller.go, called during controllers/activemqartemis_controller.go before any resources are created or updated.

**Chain Structure**:
```go
// From activemqartemis_controller.go#L214
func validate(customResource *ActiveMQArtemis, client rtclient.Client, namer Namers) (bool, retry bool) {
    validationCondition := metav1.Condition{
        Type:   brokerv1beta1.ValidConditionType,
        Status: metav1.ConditionTrue,
        Reason: brokerv1beta1.ValidConditionSuccessReason,
    }
    
    // Each validator called in sequence
    // If any validator fails, subsequent validators are skipped
    condition, retry := validateExtraMounts(customResource, client)
    if condition != nil {
        validationCondition = *condition
    }
    
    if validationCondition.Status != metav1.ConditionFalse {
        condition := validatePodDisruption(customResource)
        // ... more validators
    }
    
    // Final status set
    meta.SetStatusCondition(&customResource.Status.Conditions, validationCondition)
    return validationCondition.Status != metav1.ConditionFalse, retry
}
```

**Key Characteristics**:
1. **Short-Circuit Evaluation**: If a validator controllers/activemqartemis_controller.go, controllers/activemqartemis_controller.go
2. **Status Condition Integration**: Validation results are controllers/activemqartemis_controller.go as conditions
3. **Retry Signal**: Validators can indicate controllers/activemqartemis_controller.go (for transient errors like missing secrets)

### Individual Validation Functions

The validation chain consists of 10 validators, each checking specific configuration aspects:

#### 1. **ExtraMounts Validation** (controllers/activemqartemis_controller.go::validateExtraMounts)

**Purpose**: Validates that secrets and configmaps referenced in `extraMounts` exist and suffix-based magic behavior is correct.

**Validation Logic**:
- controllers/activemqartemis_controller.go for controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go (`-logging-config`, `-jaas-config`, `-bp`) are used correctly
- controllers/activemqartemis_controller.go - ensures each magic suffix appears only once across all extraMounts (tracked via controllers/activemqartemis_controller.go)
- **Returns retry=true** if resources don't exist yet (they may be created by another controller)

**🧪 Tested in**: controllers/activemqartemis_controller_unit_test.go::TestValidateExtraMounts, controllers/activemqartemis_controller_unit_test.go::TestValidateSuffixExtraMounts

#### 2. **PodDisruptionBudget Validation** (controllers/activemqartemis_controller.go::validatePodDisruption)

**Purpose**: Ensures PodDisruptionBudget configuration doesn't override the operator-managed selector.

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go (operator will set it automatically)
- **Fails if** user provides custom selector (would break StatefulSet matching)

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L338 - "pod disruption budget validation"

#### 3. **Broker Properties Duplicate Key Validation** (controllers/activemqartemis_controller.go::validateNoDupKeysInBrokerProperties)

**Purpose**: Detects duplicate property keys in `brokerProperties` array which would cause undefined behavior.

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go using controllers/activemqartemis_controller.go::DuplicateKeyIn()
- **Fails immediately** on first duplicate found

**🧪 Tested in**: controllers/activemqartemis_controller_unit_test.go::TestValidateBrokerPropsDuplicate, controllers/activemqartemis_controller_unit_test.go::TestValidateBrokerPropsDuplicateOnFirstEquals

#### 4. **Storage Validation** (controllers/activemqartemis_controller.go::validateStorage)

**Purpose**: Validates storage size quantities when persistence is enabled.

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go (e.g., "10Gi")
- **Fails if** quantity string is malformed

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5877 - "storage size misconfiguration handling"

**Note**: For comprehensive storage architecture including PVC creation, management, and lifecycle, see [Section 21: Storage and Persistence Architecture](#21-storage-and-persistence-architecture).

#### 5. **Acceptor Port Validation** (controllers/activemqartemis_controller.go::validateAcceptorPorts)

**Purpose**: Ensures acceptor port numbers are unique and don't conflict.

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go
- **Fails if** multiple acceptors try to use the same port

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L234 - "Acceptors with port clash detection"

#### 6. **SSL Secret Validation** (controllers/activemqartemis_controller.go::validateSSLEnabledSecrets)

**Purpose**: Validates that SSL secrets exist when SSL is enabled on acceptors/connectors.

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go
- **Returns retry=true** if secrets don't exist yet

**🧪 Tested in**: Implicitly tested in TLS/SSL configuration scenarios

#### 7. **Broker Image Version Validation** (controllers/activemqartemis_controller.go::common.ValidateBrokerImageVersion)

**Purpose**: Validates broker version specification and image pair consistency.

**Validation Logic**: See [Section 9: Version Validation Workflow](#version-validation-workflow) for complete details.

#### 8. **Reserved Labels Validation** (controllers/activemqartemis_controller.go::validateReservedLabels)

**Purpose**: Prevents users from overriding operator-managed labels.

**Validation Logic**:
- controllers/activemqartemis_controller.go like `"ActiveMQArtemis"`, `"application"`
- **Fails if** user tries to set these labels

#### 9. **Expose Mode Validation** (controllers/activemqartemis_controller.go::validateExposeModes)

**Purpose**: Validates that ingress mode is not used on OpenShift (where routes are preferred).

**Validation Logic**:
- controllers/activemqartemis_controller.go
- controllers/activemqartemis_controller.go
- **Issues warning** (not failure) when ingress is used on OpenShift

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L8560 - "Ingress in OpenShift validation"

#### 10. **Environment Variable Validation** (controllers/activemqartemis_controller.go::validateEnvVars)

**Purpose**: Prevents users from using `valueFrom` on environment variables that the operator mutates.

**Validation Logic**:
- controllers/activemqartemis_controller.go (`JAVA_OPTS`, `JAVA_ARGS_APPEND`, `DEBUG_ARGS`)
- controllers/activemqartemis_controller.go (which would be overwritten by operator)
- **Fails if** protected variables use `valueFrom`

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3785 - "LoggerProperties - Direct use of internal env vars validation"

### Validation Error Handling

The validation system provides clear, actionable error messages and manages transient vs permanent failures. For comprehensive coverage of all status conditions and error handling mechanisms, see [Section 22: Error Handling and Status Management](#22-error-handling-and-status-management).

**controllers/activemqartemis_controller.go**: Validation results are exposed as a `Valid` condition on the CR status:

```yaml
status:
  conditions:
  - type: Valid
    status: "True" | "False" | "Unknown"
    reason: "ValidConditionSuccessReason" | "ValidConditionFailureReason" | "ValidConditionInvalidXxx"
    message: "Human-readable description of validation issue"
    observedGeneration: 123
```

**Retry Behavior**:
- **Permanent Failures** (`retry=false`): Validation will not pass without CR changes (e.g., duplicate keys, invalid quantities)
- **Transient Failures** (`retry=true`): Validation may pass on next reconciliation (e.g., waiting for secrets to be created)

**controllers/activemqartemis_controller.go**: If validation fails, the reconciler controllers/activemqartemis_controller.go and controllers/activemqartemis_controller.go, preventing deployment of invalid configurations.

**Status Condition Reasons** (examples):
- `ValidConditionSuccessReason`: All validations passed
- `ValidConditionFailureReason`: Generic validation failure
- `ValidConditionFailedDuplicateBrokerPropertiesKey`: Duplicate property key detected
- `ValidConditionInvalidVersionReason`: Version doesn't resolve
- `ValidConditionPDBNonNilSelectorReason`: PodDisruptionBudget has custom selector
- `ValidConditionInvalidInternalVarUsage`: Protected environment variable misused

---

## 11. Multi-Namespace Deployment

> **Note**: For deployment instructions and configuration examples, see the deploy/README.md and deploy/cluster_wide_install_opr.sh. This section focuses on the internal namespace watching architecture.

**🧪 Tested in**: controllers/controllermanager_test.go::controllermanager_test.go#L36 - "operator namespaces test" (8 scenarios covering all watching modes)

The operator supports multiple deployment models for namespace watching, allowing it to manage broker CRs in a single namespace, multiple specific namespaces, or all namespaces cluster-wide. This flexibility enables both multi-tenant and cluster-wide deployment patterns.

### Namespace Watching Modes

The operator supports four distinct namespace watching configurations:

#### 1. **Single Namespace Mode** (Default)

**Behavior**: Operator watches only the namespace where it's deployed.

**main.go**: When pkg/sdkk8sutil/sdkk8sutil.go main.go, the operator is main.go.

**main.go**: The controller manager main.go, restricting cache and watch operations to that namespace.

**Use Case**: Namespace-scoped operator deployment with Role/RoleBinding (not ClusterRole).

#### 2. **Multiple Specific Namespaces Mode**

**Behavior**: Operator watches a comma-separated list of specific namespaces.

**main.go**: pkg/utils/common/common.go, parsed by main.go::ResolveWatchNamespaceForManager().

**main.go**: The reconciler main.go and main.go, enabling main.go.

**Use Case**: Multi-tenant deployments where the operator manages brokers in specific tenant namespaces.

**Configuration Example**:
```yaml
env:
- name: WATCH_NAMESPACE
  value: "tenant-a,tenant-b,tenant-c"
```

#### 3. **All Namespaces Mode** (Cluster-Wide)

**Behavior**: Operator watches all namespaces in the cluster.

**main.go**: pkg/utils/common/common.go, indicating main.go.

**main.go**: When watch list is nil, main.go on the controller manager, enabling main.go.

**Requirements**: Requires ClusterRole and ClusterRoleBinding with cluster-wide permissions.

**Use Case**: Cluster-wide broker management with central operator instance.

#### 4. **Restricted Namespace Mode**

**Behavior**: Operator watches a different single namespace than where it's deployed.

**main.go**: `WATCH_NAMESPACE` main.go, and main.go.

**main.go**: When main.go, it's treated as main.go with main.go.

**Use Case**: Operator deployed in `operators` namespace but managing brokers in `production` namespace.

### WATCH_NAMESPACE Configuration

The `WATCH_NAMESPACE` environment variable is the primary mechanism for controlling namespace scope:

**main.go**: During operator startup, pkg/sdkk8sutil/sdkk8sutil.go::GetWatchNamespace() pkg/sdkk8sutil/sdkk8sutil.go and main.go.

**main.go**: The operator pkg/sdkk8sutil/sdkk8sutil.go by pkg/sdkk8sutil/sdkk8sutil.go at `/var/run/secrets/kubernetes.io/serviceaccount/namespace`.

**main.go**: The reconciler main.go (`OPERATOR_NAMESPACE` and `OPERATOR_WATCH_NAMESPACE`) for main.go.

**Configuration Patterns**:

| WATCH_NAMESPACE Value | Behavior | Required RBAC |
|----------------------|----------|---------------|
| (unset) | Single namespace (operator's NS) | Role |
| `"my-namespace"` | Single namespace (specified) | Role in target NS |
| `"ns1,ns2,ns3"` | Multiple specific namespaces | Roles in each NS |
| `"*"` | All namespaces (cluster-wide) | ClusterRole |
| `""` | All namespaces (cluster-wide) | ClusterRole |

**Deployment Examples**:

```yaml
# Single namespace (default)
env:
- name: WATCH_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace

# Multiple namespaces
env:
- name: WATCH_NAMESPACE
  value: "production,staging,development"

# Cluster-wide
env:
- name: WATCH_NAMESPACE
  value: "*"
```

### Manager Configuration

The controller-runtime manager is configured based on the resolved namespace mode:

**main.go**: The main.go::ctrl.Options struct is main.go based on the watch mode.

**main.go**: The main.go in main.go determines which namespaces the manager's cache will watch:

```go
// From main.go#L193
isLocal, watchList := common.ResolveWatchNamespaceForManager(oprNamespace, watchNamespace)
if isLocal {
    // Single namespace mode
    mgrOptions.Cache.DefaultNamespaces = map[string]cache.Config{
        oprNamespace: {}}
} else {
    if watchList != nil {
        // Multiple namespace mode
        nsMap := map[string]cache.Config{}
        for _, ns := range watchList {
            nsMap[ns] = cache.Config{}
        }
        mgrOptions.Cache.DefaultNamespaces = nsMap
    } else {
        // Cluster-wide mode - no restrictions
    }
}
```

**Cross-Namespace Resource Access**: When operating in multi-namespace mode, controllers can access resources across namespaces by controllers/activemqartemis_controller.go using controllers/activemqartemis_controller.go::types.NamespacedName. Example: the controllers/activemqartemisscaledown_controller.go that controllers/activemqartemisscaledown_controller.go when not in local-only mode.

**Deployment Scripts**:
- **Namespace-scoped**: deploy/install_opr.sh::deploy/install_opr.sh - Deploys with deploy/install_opr.sh and deploy/install_opr.sh
- **Cluster-wide**: deploy/cluster_wide_install_opr.sh::deploy/cluster_wide_install_opr.sh - Prompts for WATCH_NAMESPACE and deploys with deploy/cluster_wide_install_opr.sh and deploy/cluster_wide_install_opr.sh

**Manager Configuration Details** (main.go):
- Namespace list
- Metrics bind address
- Health probe bind address
- Leader election settings
- Lease duration and renewal settings

## 12. High Availability and Resilience

> **Note**: For user-facing HA configuration examples, see the [operator help documentation](../help/operator.md#configuring-poddisruptionbudget-for-broker-deployment). This section focuses on the internal HA feature implementation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L336 - pod disruption budget (5 scenarios), controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4171 - affinity (3 scenarios: PodAffinity, PodAntiAffinity, NodeAffinity), controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4737 - tolerations (3 scenarios)

The operator provides multiple mechanisms to ensure high availability and resilience of broker deployments through Kubernetes native features. These mechanisms protect brokers from disruptions, distribute workload across failure domains, and allow scheduling on specialized nodes.

### PodDisruptionBudget Configuration

PodDisruptionBudgets (PDBs) limit the number of pods that can be down simultaneously during voluntary disruptions like node drains or upgrades.

**controllers/activemqartemis_reconciler.go**: When api/v1beta1/activemqartemis_types.go, the reconciler controllers/activemqartemis_reconciler.go during controllers/activemqartemis_reconciler.go.

**controllers/activemqartemis_reconciler.go**: The operator:

1. **controllers/activemqartemis_reconciler.go**: Looks for controllers/activemqartemis_reconciler.go with naming pattern `<cr-name>-pdb`

2. **controllers/activemqartemis_reconciler.go**: If none exists, controllers/activemqartemis_reconciler.go with controllers/activemqartemis_reconciler.go

3. **controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go (minAvailable or maxUnavailable)

4. **controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go with `ActiveMQArtemis: <cr-name>` label

5. **controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go for reconciliation

**Automatic Selector Override**: The operator controllers/activemqartemis_reconciler.go to ensure the PDB targets the correct StatefulSet pods, even if the user provides a custom selector in the CR.

**Configuration Example**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: ha-broker
spec:
  deploymentPlan:
    size: 3
    podDisruptionBudget:
      minAvailable: 2  # Keep at least 2 brokers running
```

### Affinity and Anti-Affinity

Affinity rules control pod scheduling to distribute brokers across nodes, zones, or co-locate with specific workloads.

**controllers/activemqartemis_reconciler.go**: When the reconciler controllers/activemqartemis_reconciler.go, it controllers/activemqartemis_reconciler.go to include user-specified affinity rules from api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.affinity.

**Affinity Types Supported**:

1. **Node Affinity**: Schedule pods on nodes with specific labels
   - controllers/activemqartemis_reconciler.go
   - **Use Case**: Dedicate specific nodes to broker workload
2. **Pod Affinity**: Co-locate pods with other pods matching criteria
   - controllers/activemqartemis_reconciler.go
3. **Pod Anti-Affinity**: Spread pods across failure domains
   - controllers/activemqartemis_reconciler.go
   - **Use Case**: Ensure brokers run on different nodes for resilience

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4172 - "Pod Affinity"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4254 - "Pod AntiAffinity"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4333 - "Node Affinity"

**Example - Zone-Level Anti-Affinity**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: ha-broker
spec:
  deploymentPlan:
    size: 3
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
            - key: ActiveMQArtemis
              operator: In
              values:
              - ha-broker
          topologyKey: topology.kubernetes.io/zone
```

### Tolerations and Node Selection

Tolerations allow pods to schedule on tainted nodes, while node selectors constrain scheduling to specific nodes.

**controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go (via api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.tolerations) in controllers/activemqartemis_reconciler.go.

**controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go (via api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.nodeSelector) in controllers/activemqartemis_reconciler.go.

**Use Cases**:
- **Tolerations**: Schedule brokers on nodes with specific taints (e.g., dedicated GPU nodes, spot instances)
- **Node Selector**: Restrict brokers to nodes with specific hardware (e.g., SSD storage, high memory)

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5162 - "Tolerations Test - Passing in 2 tolerations"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4843 - "Tolerations Existing Cluster - Required add/remove"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5136 - "Node Selector Test - Passing in multiple labels"

**Example - Dedicated Nodes**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: prod-broker
spec:
  deploymentPlan:
    size: 2
    nodeSelector:
      workload-type: messaging
      storage-type: ssd
    tolerations:
    - key: "dedicated"
      operator: "Equal"
      value: "messaging"
      effect: "NoSchedule"
```

### TopologySpreadConstraints

TopologySpreadConstraints provide fine-grained control over pod distribution across topology domains (nodes, zones, regions).

**controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go from api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.topologySpreadConstraints in controllers/activemqartemis_reconciler.go.

**Advantages over Anti-Affinity**:
- More flexible spreading across topology domains
- Supports skew limits (allows controlled imbalance)
- Can define multiple constraints simultaneously

**🧪 Tested in**: controllers/activemqartemis_reconciler_test.go::activemqartemis_reconciler_test.go#L1145 - "TestNewPodTemplateSpecForCR_IncludesTopologySpreadConstraints"

**Example - Multi-Zone Distribution**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: ha-broker
spec:
  deploymentPlan:
    size: 6
    topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          ActiveMQArtemis: ha-broker
```

---

## 13. Probe Configuration

> **Note**: For user-facing probe configuration examples, see the [operator help documentation](../help/operator.md#probe-configuration). This section focuses on the internal probe implementation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5237 - liveness probes (5 scenarios), controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5385 - readiness probes (4 scenarios), controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5477 - startup probes (2 scenarios)

The operator configures Kubernetes liveness, readiness, and startup probes for broker containers, with sensible defaults and full override capability. These probes ensure Kubernetes can properly manage broker lifecycle and health.

### Default Probe Configurations

The operator provides default probe configurations optimized for ActiveMQ Artemis brokers:

**controllers/activemqartemis_reconciler.go**: When the user doesn't api/v1beta1/activemqartemis_types.go, the operator controllers/activemqartemis_reconciler.go:

```go
// From activemqartemis_reconciler.go#L2650
livenessProbe.InitialDelaySeconds = defaultLivenessProbeInitialDelay  // 5 seconds
livenessProbe.TimeoutSeconds = 5
livenessProbe.ProbeHandler = corev1.ProbeHandler{
    TCPSocket: &corev1.TCPSocketAction{
        Port: intstr.FromInt(TCPLivenessPort),  // 8161
    },
}
```

**controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go is set to 5 seconds, balancing fast startup detection with avoiding false negatives.

**controllers/activemqartemis_reconciler.go**: Similar to liveness, but controllers/activemqartemis_reconciler.go.

**controllers/activemqartemis_reconciler.go**: By default, controllers/activemqartemis_reconciler.go unless explicitly configured by the user.

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4973 - "Default Liveness Probe"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5268 - "Default Readiness Probe"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5374 - "Default Startup Probe"

### Probe Override Mechanism

Users can completely override any probe configuration through the CR:

**controllers/activemqartemis_reconciler.go**: The reconciler controllers/activemqartemis_reconciler.go and applies it through controllers/activemqartemis_reconciler.go:

```go
// From activemqartemis_reconciler.go#L2199-2203
container.StartupProbe = reconciler.configureStartupProbe(container, customResource.Spec.DeploymentPlan.StartupProbe)
if !common.IsRestricted(customResource) {
    container.LivenessProbe = reconciler.configureLivenessProbe(container, customResource.Spec.DeploymentPlan.LivenessProbe)
}
container.ReadinessProbe = reconciler.configureReadinessProbe(container, customResource.Spec.DeploymentPlan.ReadinessProbe)
```

**controllers/activemqartemis_reconciler.go**: Probe configuration is controllers/activemqartemis_reconciler.go within controllers/activemqartemis_reconciler.go.

**Override Example**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: custom-probes-broker
spec:
  deploymentPlan:
    livenessProbe:
      tcpSocket:
        port: 61616
      initialDelaySeconds: 30
      periodSeconds: 20
    readinessProbe:
      httpGet:
        path: /console/jolokia
        port: 8161
      initialDelaySeconds: 10
```

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4831 - "Override Liveness Probe No Exec"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4921 - "Override Liveness Probe Exec"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5141 - "Override Readiness Probe No Exec"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5228 - "Override Readiness Probe GRPC"

### Probe Types and Use Cases

The operator supports all Kubernetes probe types:

#### 1. **Exec Probes**

**controllers/activemqartemis_reconciler.go**: Runs a controllers/activemqartemis_reconciler.go (for readiness, liveness uses controllers/activemqartemis_reconciler.go):

```yaml
livenessProbe:
  exec:
    command: ["/bin/bash", "-c", "/opt/amq/bin/readinessProbe.sh"]
```

**Use Case**: Default probe type, uses broker-provided health check scripts.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5186 - "Override Readiness Probe Exec"

#### 2. **TCP Socket Probes**

**Configuration**: Opens a TCP connection to a port:

```yaml
livenessProbe:
  tcpSocket:
    port: 61616  # CORE protocol port
```

**Use Case**: Simple connectivity check, verifies broker port is accepting connections.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5011 - "Override Liveness Probe Default TCPSocket"

#### 3. **HTTP Get Probes**

**Configuration**: Performs an HTTP GET request:

```yaml
readinessProbe:
  httpGet:
    path: /console/jolokia/read/org.apache.activemq.artemis:broker=*
    port: 8161
```

**Use Case**: Check broker readiness via Jolokia REST API.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5055 - "Override Liveness Probe Default HTTPGet"

#### 4. **GRPC Probes**

**Configuration**: Uses the gRPC health checking protocol:

```yaml
livenessProbe:
  grpc:
    port: 9090
```

**Use Case**: Advanced health checking for gRPC-enabled brokers.

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5097 - "Override Liveness Probe Default GRPC"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5228 - "Override Readiness Probe GRPC"

**Probe Selection Guidelines**:
- **Exec**: Most reliable, uses broker's own health check logic
- **TCP**: Simplest, just checks port availability
- **HTTP**: Good for monitoring specific broker metrics
- **GRPC**: For advanced deployments with gRPC infrastructure

---

## 14. Logging Configuration

> **Note**: For user-facing logging configuration, see the [logging guidelines](../help/logging-guidelines.md). This section focuses on the internal logging implementation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6122 - "LoggerProperties" test context (5 scenarios)

The operator allows users to customize broker logging configuration while protecting internal environment variables that it manages. This system ensures logging properties can be customized without breaking operator functionality.

### Custom Logging Properties

Users can provide custom logging configuration via ConfigMaps or Secrets:

**Logging Resource Detection**: During pod template creation, the reconciler controllers/activemqartemis_reconciler.go by calling controllers/activemqartemis_reconciler.go::getLoggingConfigExtraMountPath().

**Logging ConfigMap Pattern**: Resources in `extraMounts` ending with controllers/activemqartemis_reconciler.go are controllers/activemqartemis_reconciler.go and mapped to `logging.properties`.

**Volume Mount Application**: Logging resources from controllers/activemqartemis_reconciler.go are automatically mounted into the broker container.

**Configuration Example**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-logging
data:
  logging.properties: |
    # Custom logging configuration
    logger.org.apache.activemq.artemis.level=DEBUG
    logger.org.apache.activemq.artemis.core.server.level=TRACE
---
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: broker
spec:
  deploymentPlan:
    extraMounts:
      configMaps:
      - custom-logging
```

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6328 - "logging configmap validation"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6368 - "logging secret and configmap validation"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6465 - "Expect vol mount for logging configmap deployed"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6532 - "Expect vol mount for logging secret deployed"

### Protected Environment Variables

The operator protects specific environment variables from misuse to prevent conflicts with its internal logic:

**Protected Variables**: The controllers/activemqartemis_controller.go identifies controllers/activemqartemis_controller.go:

```go
// From activemqartemis_controller.go#L434
internalVarNames := map[string]string{
    debugArgsEnvVarName:      debugArgsEnvVarName,      // "DEBUG_ARGS"
    javaOptsEnvVarName:       javaOptsEnvVarName,       // "JAVA_OPTS"
    javaArgsAppendEnvVarName: javaArgsAppendEnvVarName, // "JAVA_ARGS_APPEND"
}
```

**controllers/activemqartemis_controller.go**: The validator controllers/activemqartemis_controller.go (ConfigMapKeyRef, SecretKeyRef, etc.) which controllers/activemqartemis_controller.go.

**controllers/activemqartemis_controller.go**: If protected variables use `valueFrom`, controllers/activemqartemis_controller.go suggesting the workaround.

**Why This Matters**: The operator controllers/activemqartemis_reconciler.go (e.g., adding broker properties paths, JVM agents). Using `valueFrom` would cause the operator's additions to be lost.

**Correct Usage Pattern**:
```yaml
# ❌ WRONG - valueFrom on protected variable
env:
- name: JAVA_ARGS_APPEND
  valueFrom:
    configMapKeyRef:
      name: my-config
      key: java-args

# ✅ CORRECT - Use direct value and reference another var
env:
- name: MY_CUSTOM_ARGS
  valueFrom:
    configMapKeyRef:
      name: my-config
      key: java-args
- name: JAVA_ARGS_APPEND
  value: "$(MY_CUSTOM_ARGS) -Dmy.property=value"
```

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6124 - "test validate can pick up all internal vars misusage"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6180 - "validate user directly using internal env vars"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6241 - "custom logging not to override JAVA_ARGS_APPEND"

---

## 15. Certificate Management

> **Note**: For user-facing certificate configuration examples, see the [send_receive_ingress_pem tutorial](../tutorials/send_receive_ingress_pem.md). This section focuses on the internal certificate management architecture.

**🧪 Tested in**: controllers/activemqartemis_controller_cert_manager_test.go::activemqartemis_controller_cert_manager_test.go#L68 - cert-manager integration (4+ scenarios)

The operator integrates with cert-manager to provide automated certificate lifecycle management for TLS-enabled brokers, particularly in restricted mode where mTLS is enforced.

### cert-manager Integration

The operator leverages cert-manager for certificate provisioning and rotation:

**controllers/activemqartemis_reconciler.go**: When controllers/activemqartemis_reconciler.go, the reconciler controllers/activemqartemis_reconciler.go that controllers/activemqartemis_reconciler.go.

**controllers/activemqartemis_reconciler.go**: The operator controllers/activemqartemis_reconciler.go, which are then controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

**pkg/utils/common/common.go**: The operator expects certificates with pkg/utils/common/common.go:
- pkg/utils/common/common.go::DefaultOperatorCertSecretName: Operator's client certificate
- pkg/utils/common/common.go::DefaultOperatorCASecretName: CA certificate for validation
- pkg/utils/common/common.go::DefaultOperandCertSecretName: Broker's server certificate

**cert-manager Resources** (created by user, not operator):
```yaml
# Example Certificate resources
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: broker-cert
spec:
  secretName: broker-cert-secret  # Matches DefaultOperandCertSecretName
  issuerRef:
    name: my-issuer
    kind: ClusterIssuer
  commonName: broker.example.com
  dnsNames:
  - broker.example.com
  - "*.broker.svc.cluster.local"
```

**🧪 Tested in**: controllers/activemqartemis_controller_cert_manager_test.go::activemqartemis_controller_cert_manager_test.go#L96 - "Broker deployment with cert-manager"

### Certificate Lifecycle

The operator integrates with cert-manager's certificate lifecycle management:

**Certificate Generation**: When cert-manager provisions certificates, it creates secrets with certificate data (ca.crt, tls.crt, tls.key in PEM format).

**Certificate Mounting**: The operator controllers/activemqartemis_reconciler.go at standard paths like `/amq/extra/secrets/<secret-name>/`.

**Certificate Usage**: For restricted mode, certificates are controllers/activemqartemis_reconciler.go, eliminating the need for JKS conversion.

**Certificate Rotation**: When cert-manager rotates certificates:
1. cert-manager updates the secret contents
2. The operator controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go
3. controllers/activemqartemis_reconciler.go by updating the StatefulSet template
4. Kubernetes performs rolling update of broker pods

**🧪 Tested in**: controllers/activemqartemis_controller_cert_manager_test.go::activemqartemis_controller_cert_manager_test.go#L465 - "broker certificate rotation"

### TLS Configuration Patterns

The operator supports multiple TLS configuration patterns:

#### 1. **Restricted Mode mTLS** (Recommended)

**controllers/activemqartemis_reconciler.go**: All management interfaces controllers/activemqartemis_reconciler.go:

```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: secure-broker
spec:
  restricted: true
  deploymentPlan:
    size: 1
    image: placeholder
```

**Certificate Requirements**: Three certificates needed:
1. Broker server certificate (for broker identity)
2. Operator client certificate (for operator-to-broker communication)
3. CA certificate (for mutual validation)

**🧪 Tested in**: controllers/controll_plane_test.go::controll_plane_test.go#L103 - "restricted rbac"

#### 2. **Acceptor-Level TLS**

**Configuration**: TLS controllers/activemqartemis_reconciler.go via api/v1beta1/activemqartemis_types.go::acceptor.sslEnabled:

```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: tls-broker
spec:
  acceptors:
  - name: amqps
    protocols: core
    port: 61617
    sslEnabled: true
    sslSecret: acceptor-tls-secret
```

**Certificate Mounting**: controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L89 - "tls secret reuse" test context, controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7213 - "acceptor" tests

#### 3. **Console TLS**

**Configuration**: Console controllers/activemqartemis_reconciler.go via api/v1beta1/activemqartemis_types.go::console.sslEnabled:

```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: console-tls-broker
spec:
  console:
    expose: true
    sslEnabled: true
    sslSecret: console-tls-secret
```

**Certificate Handling**: controllers/activemqartemis_controller.go, controllers/activemqartemis_reconciler.go, and controllers/activemqartemis_reconciler.go for the Jetty server.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1890 - "Exposing secured console"

---

## 16. Operational Controls

> **Note**: For operational procedures and troubleshooting, see the [operator help documentation](../help/operator.md). This section focuses on the internal operational control mechanisms.

**🧪 Tested in**: controllers/activemqartemis_controller_block_reconcile_test.go::activemqartemis_controller_block_reconcile_test.go#L36 - block reconcile annotation (2 scenarios)

The operator provides operational controls for managing broker lifecycles, pausing reconciliation for maintenance, tracking resource ownership, and recovery from failures.

### Block Reconcile Annotation

The block reconcile feature allows operators to temporarily pause reconciliation for maintenance or troubleshooting:

**Annotation Detection**: The reconciler controllers/activemqartemis_controller.go at the start of each reconciliation:

```go
// From activemqartemis_controller.go#L169
if val, present := customResource.Annotations[common.BlockReconcileAnnotation]; present {
    if boolVal, err := strconv.ParseBool(val); err == nil {
        reconcileBlocked = boolVal
    }
}
```

**Reconciliation Skip**: When the annotation is true, controllers/activemqartemis_controller.go, preserving the current cluster state.

**Status Condition**: A pkg/utils/common/common.go:

```yaml
status:
  conditions:
  - type: ReconcileBlocked
    status: "True"
    reason: "AnnotationPresent"
    message: "Reconciliation blocked by annotation"
```

**Usage**:
```bash
# Block reconciliation
kubectl annotate activemqartemis broker \
  broker.amq.io/block-reconcile=true

# Resume reconciliation
kubectl annotate activemqartemis broker \
  broker.amq.io/block-reconcile-
```

**Use Cases**:
- Manual intervention/debugging required
- Temporary configuration freeze during testing
- Preventing operator changes during maintenance window

**🧪 Tested in**: 
- controllers/activemqartemis_controller_block_reconcile_test.go::activemqartemis_controller_block_reconcile_test.go#L47 - "deploy, annotate, verify"
- controllers/activemqartemis_controller_block_reconcile_test.go::activemqartemis_controller_block_reconcile_test.go#L152 - "annotate, deploy, verify"

### Resource Tracking and Adoption

The operator tracks all resources it creates and can adopt orphaned resources:

**Desired Resource Tracking**: As the reconciler controllers/activemqartemis_controller.go, it controllers/activemqartemis_reconciler.go via `trackDesired()` calls throughout reconciliation:

```go
// From activemqartemis_reconciler.go#L418
reconciler.trackDesired(desired)  // Adds resource to desired state map
```

**Deployed Resource Tracking**: The operator controllers/activemqartemis_reconciler.go that it queries during reconciliation via `cloneOfDeployed()`.

**Owner Reference Management**: The operator pkg/resources/k8s_actions.go via pkg/resources/k8s_actions.go, called during controllers/activemqartemis_reconciler.go, enabling:
- Automatic cleanup on CR deletion (cascade delete via pkg/resources/k8s_actions.go)
- Resource adoption when owner references are lost

**Secret Adoption**: The operator controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1070 - "default user credential secret"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1231 - "default user credential secret with values in CR"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3976 - "reconcile verify adopt internal secret that has lost owner ref"

### Recovery Patterns

The operator implements several recovery patterns to handle failures gracefully:

#### 1. **StatefulSet Recreation**

**Recovery Logic**: If the StatefulSet is manually deleted, the operator controllers/activemqartemis_reconciler.go automatically during reconciliation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3350 - "SS delete recreate Test"

#### 2. **PVC Persistence**

**PVC Retention**: When StatefulSet is deleted, controllers/activemqartemis_reconciler.go (no owner reference set), allowing data recovery after recreation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3411 - "PVC no gc test"

**Note**: For comprehensive storage architecture including PVC creation, adoption, and dynamic toggling, see [Section 21: Storage and Persistence Architecture](#21-storage-and-persistence-architecture).

#### 3. **Cascade Delete**

**Foreground Deletion**: The operator supports cascade delete with foreground deletion policy, ensuring all owned resources are deleted before the CR is removed.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L8482 - "cascade delete foreground test"

#### 4. **ImagePullBackOff Recovery**

**Image Update Recovery**: If a broker pod enters ImagePullBackOff state, the operator can recover by updating to a valid image, triggering pod replacement.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2861 - "deploy ImagePullBackOff update delete ok"

**Recovery Workflow**:
```
1. Detect failure (missing resource, pod error)
   ↓
2. Identify cause via status conditions
   ↓
3. Apply corrective action (recreate, update, adopt)
   ↓
4. Verify recovery via status check
   ↓
5. Update CR status conditions
```

The combination of resource tracking, status conditions, and automatic recovery ensures the operator can handle common operational failures without manual intervention.

---

## 17. Console and Web UI Architecture

> **Note**: For user-facing console configuration, see the [operator help documentation](../help/operator.md#console-configuration). This section focuses on the internal console implementation.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go - Console Configuration (4 scenarios)

The operator manages the ActiveMQ Artemis web console (Hawtio-based management interface) and Jolokia JMX-HTTP bridge, handling exposure, SSL configuration, and service port management for metrics scraping.

### Console Exposure Mechanisms

The console can be exposed externally through platform-specific resources:

**controllers/activemqartemis_reconciler.go**: During reconciliation, controllers/activemqartemis_reconciler.go::ProcessConsole() is called to handle console configuration.

**controllers/activemqartemis_reconciler.go**:

1. **controllers/activemqartemis_reconciler.go**: If controllers/activemqartemis_reconciler.go::restricted: true, controllers/activemqartemis_reconciler.go (Jolokia remains for operator management)

2. **controllers/activemqartemis_reconciler.go**: If api/v1beta1/activemqartemis_types.go::console.expose: true, the operator controllers/activemqartemis_reconciler.go

3. **controllers/activemqartemis_reconciler.go**: The operator controllers/activemqartemis_reconciler.go to choose between Route and Ingress

**Platform-Specific Exposure**:

**OpenShift** (controllers/activemqartemis_reconciler.go):
- controllers/activemqartemis_reconciler.go for console services
- pkg/resources/routes/route.go (passthrough for SSL at L43, nil for non-SSL at L47)
- pkg/resources/routes/route.go

**Kubernetes** (controllers/activemqartemis_reconciler.go):
- controllers/activemqartemis_reconciler.go with nginx ingress class
- pkg/resources/ingresses/ingress.go when SSL enabled
- pkg/resources/ingresses/ingress.go (brokerHost at L68, domain at L70-71, host set at L74)

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1820 - "checking console target service port name for metrics"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1890 - "Exposing secured console"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2332 - "Expose mode test" (Ingress/Route)

### Jolokia Integration and Metrics Ports

The operator configures Jolokia for JMX-over-HTTP access and ensures proper service port configuration for Prometheus scraping:

**controllers/activemqartemis_reconciler.go**: The headless service is created with pkg/resources/serviceports/service_port.go:

1. **pkg/resources/serviceports/service_port.go**: Port `8161` named `console-jolokia` for web console access
2. **pkg/resources/serviceports/service_port.go**: Port `8778` named `jolokia` for metrics scraping

**controllers/activemqartemis_reconciler.go**: In controllers/activemqartemis_reconciler.go::configureConsoleExposure(), each broker pod gets controllers/activemqartemis_reconciler.go with controllers/activemqartemis_reconciler.go (e.g., `wconsj-0`, `wconsj-1`) created in a controllers/activemqartemis_reconciler.go.


**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1820 - Validates console services have correct port names and numbers for metrics scraping

### Console SSL Configuration

The operator supports SSL for console access with user-specified certificates:

**controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go::ProcessConsole() handles console configuration, controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

**Certificate Resolution** (controllers/activemqartemis_reconciler.go):

1. **controllers/activemqartemis_reconciler.go**: Operator controllers/activemqartemis_reconciler.go from `SecretsConsoleNameBuilder` (pattern: `{broker-name}-console-secret`)
2. **controllers/activemqartemis_reconciler.go**: If api/v1beta1/activemqartemis_types.go::console.sslSecret is specified, controllers/activemqartemis_reconciler.go
3. **controllers/activemqartemis_controller.go**: During validation, controllers/activemqartemis_controller.go and controllers/activemqartemis_controller.go

**Volume Configuration**:
- **controllers/activemqartemis_reconciler.go**: Console SSL secrets are controllers/activemqartemis_reconciler.go when controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: Volume mounts are controllers/activemqartemis_reconciler.go at standard paths like `/etc/{secret-name}-volume/`

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1890 - "Exposing secured console"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2197 - "Exposing secured console with user specified secret"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2764 - "ssl console secret validation"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3799 - "deploy broker with ssl enabled console"

**Configuration Example**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: console-broker
spec:
  console:
    expose: true
    sslEnabled: true
    sslSecret: my-console-tls  # Optional, auto-generated if omitted
  ingressDomain: apps.example.com  # For Kubernetes Ingress hostname
```

---

## 18. Networking and Exposure Architecture

> **Note**: For user-facing acceptor and connector configuration, see the [operator help documentation](../help/operator.md#acceptors-and-connectors). This section focuses on the internal networking architecture.

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7213 - "With deployed controller - acceptor" (8 scenarios)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7466 - "acceptor/connector update no owner ref" (2 scenarios)

The operator translates CR acceptor and connector specifications into broker configuration and Kubernetes networking resources (Services, Routes, Ingress).

### Acceptor Architecture

Acceptors define how clients connect to the broker. The operator processes acceptor specifications and creates corresponding broker configuration and Kubernetes resources.

**controllers/activemqartemis_reconciler.go**: During reconciliation, controllers/activemqartemis_reconciler.go::ProcessAcceptorsAndConnectors() handles acceptor configuration.

**controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go::generateAcceptorsString() function:

1. **controllers/activemqartemis_reconciler.go** from api/v1beta1/activemqartemis_types.go::spec.acceptors
2. **controllers/activemqartemis_reconciler.go** with acceptor name, protocol, port, SSL settings
3. **controllers/activemqartemis_reconciler.go**: If api/v1beta1/activemqartemis_types.go::acceptor.sslEnabled, adds SSL parameters
4. **controllers/activemqartemis_reconciler.go**: Uses `acceptor.bindToAllInterfaces` (checked at L767) or defaults to `ACCEPTOR_IP`
5. **controllers/activemqartemis_reconciler.go**: Appends SSL configuration flags from `generateCommonSSLFlags`

**controllers/activemqartemis_reconciler.go**: In controllers/activemqartemis_reconciler.go::configureAcceptorsExposure(), for exposed acceptors:

1. **controllers/activemqartemis_reconciler.go**: Iterates over `customResource.Spec.Acceptors`
2. **controllers/activemqartemis_reconciler.go** per acceptor with name pattern `{broker}-{acceptor}-{ordinal}-svc`
3. **controllers/activemqartemis_reconciler.go**: Adds service to desired resources
4. **controllers/activemqartemis_reconciler.go**: If `acceptor.Expose`, creates Route or Ingress

**SSL Secret Mounting**:

- **controllers/activemqartemis_reconciler.go**: Scans acceptors for SSL-enabled acceptors
- **controllers/activemqartemis_reconciler.go**: SSL secrets controllers/activemqartemis_reconciler.go at `/amq/extra/secrets/{secret-name}/`
- **controllers/activemqartemis_reconciler.go**: If `acceptor.TrustSecret` specified, controllers/activemqartemis_reconciler.go
- **controllers/activemqartemis_reconciler.go**: Configuration string includes SSL flags from L794

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7215 - "Add acceptor via update"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7289 - "Checking acceptor service and route/ingress while toggle expose"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7585 - "Testing acceptor bindToAllInterfaces"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7764 - "Testing acceptor keyStoreProvider"

### Connector Architecture

Connectors define how the broker connects to other brokers or external systems.

**controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go::generateConnectorsString() function builds connector configuration similar to acceptors, with SSL and networking settings.

**controllers/activemqartemis_reconciler.go**: In controllers/activemqartemis_reconciler.go::configureConnectorsExposure(), like acceptors, exposed connectors controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go for external connectivity.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L7527 - "with existing connector service"

### Expose Mode Selection

The operator chooses between Ingress and Route based on platform detection:

**controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go controllers/activemqartemis_reconciler.go along with user-specified api/v1beta1/activemqartemis_types.go::ExposeMode to determine exposure strategy.

**Decision Logic** (controllers/activemqartemis_reconciler.go):
```go
exposeWithRoute := (exposeMode == nil && reconciler.isOnOpenShift) || 
                   (exposeMode != nil && *exposeMode == brokerv1beta1.ExposeModes.Route)
```

**Exposure Strategy**:
```
if exposeMode == Route OR (exposeMode == nil AND isOpenShift):
    → Create Route resources
else:
    → Create Ingress resources
```

**pkg/resources/routes/route.go** (controllers/activemqartemis_reconciler.go):
- pkg/resources/routes/route.go
- pkg/resources/routes/route.go (passthrough for SSL at L43, nil for non-SSL at L47)
- pkg/resources/routes/route.go from IngressDomain

**pkg/resources/ingresses/ingress.go** (controllers/activemqartemis_reconciler.go):
- pkg/resources/ingresses/ingress.go
- pkg/resources/ingresses/ingress.go when SSL enabled
- pkg/resources/ingresses/ingress.go (brokerHost at L68, domain at L70-71, host set at L74)

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2333 - "expose with ingress mode" (HTTP)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2491 - "expose with secure ingress mode" (HTTPS)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L2648 - "expose with route mode" (OpenShift)

**Configuration Example**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: external-broker
spec:
  console:
    expose: true
    sslEnabled: true
  ingressDomain: apps.example.com
  acceptors:
  - name: amqp
    protocols: amqp
    port: 5672
    expose: true
    sslEnabled: true
    sslSecret: amqp-tls-secret
```

---

## 19. Credential and Secret Management

> **Note**: For user-facing credential configuration, see the [operator help documentation](../help/operator.md#credentials). This section focuses on the internal credential management architecture.

**🧪 Tested in**: 
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1069 - Broker Resource Tracking (4 scenarios)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3797 - Console Secret Test (3 scenarios)

The operator manages broker credentials, console SSL secrets, and external secret references with automatic generation, adoption, and owner reference tracking.

### User Credential Generation

The operator automatically generates admin credentials when not explicitly provided:

**controllers/activemqartemis_reconciler.go**: The controllers/activemqartemis_reconciler.go::ProcessCredentials() function handles credential configuration.

**Generation Flow**:

1. **controllers/activemqartemis_reconciler.go**: Uses controllers/activemqartemis_reconciler.go::SecretsCredentialsNameBuilder.Name() to get secret name (pattern: `{broker}-credentials-secret`)

2. **controllers/activemqartemis_reconciler.go**: Generates credentials with fallback logic:
   - **controllers/activemqartemis_reconciler.go**: From `spec.adminUser`, controllers/activemqartemis_reconciler.go
   - **controllers/activemqartemis_reconciler.go**: From `spec.adminPassword`, controllers/activemqartemis_reconciler.go
   - **controllers/activemqartemis_reconciler.go**: Always controllers/activemqartemis_reconciler.go
   - **controllers/activemqartemis_reconciler.go**: Always controllers/activemqartemis_reconciler.go

3. **controllers/activemqartemis_reconciler.go**: Calls controllers/activemqartemis_reconciler.go::sourceEnvVarFromSecret() which controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go

4. **controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go using controllers/activemqartemis_reconciler.go::valueFrom.secretKeyRef in controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1070 - "default user credential secret"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1231 - "default user credential secret with values in CR"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1300 - "user credential secret" (external)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1462 - "user credential secret with CR values provided"

### Console Secret Lifecycle

For SSL-enabled consoles, the operator expects user-provided TLS secrets:

**controllers/activemqartemis_reconciler.go**: When console SSL is enabled without explicit secret:

1. **controllers/activemqartemis_reconciler.go**: Looks for controllers/activemqartemis_reconciler.go
2. **controllers/activemqartemis_reconciler.go**: Creates controllers/activemqartemis_reconciler.go with self-signed certificate
3. **controllers/activemqartemis_reconciler.go**: Uses `{broker}-console-secret-{hash}` for uniqueness

**controllers/activemqartemis_reconciler.go**: The operator can controllers/activemqartemis_reconciler.go:

1. **controllers/activemqartemis_reconciler.go**: Checks if controllers/activemqartemis_reconciler.go
2. **controllers/activemqartemis_reconciler.go**: controllers/activemqartemis_reconciler.go
3. **controllers/activemqartemis_reconciler.go**: Persists the adoption

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3799 - "deploy broker with ssl enabled console" (auto-generation)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3874 - "reconcile verify internal secret owner ref"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3976 - "reconcile verify adopt internal secret that has lost owner ref"

### Secret Persistence and Stability

Generated credentials remain stable across reconciliation cycles:

**controllers/activemqartemis_reconciler.go**: Once generated, credential values controllers/activemqartemis_reconciler.go, preventing connection disruptions.

**pkg/resources/k8s_actions.go**: Owner references with pkg/resources/k8s_actions.go::Controller: true enable pkg/resources/k8s_actions.go when broker CR is deleted.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L1070 - Validates secrets remain unchanged across reconciliations ([L1212-1215](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/controllers/activemqartemis_controller_test.go#L1212-1215) verifies credential values match original)

---

## 20. Resource Metadata Management

> **Note**: For user-facing annotation and label configuration, see the [operator help documentation](../help/operator.md#labels-and-annotations).

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4490 - Annotations Test
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4594 - Labels Test (2 scenarios)

The operator manages pod annotations and labels, propagating custom metadata while protecting operator-managed labels from user override.

### Custom Annotations Propagation

Users can add custom annotations to broker pods for integration with monitoring, security policies, or other systems:

**controllers/activemqartemis_reconciler.go**: During pod template creation, annotations from api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.annotations are controllers/activemqartemis_reconciler.go via pkg/resources/pods/pod.go::MakePodTemplateSpec().

**pkg/resources/pods/pod.go**: Custom annotations are pkg/resources/pods/pod.go via `ApplyAnnotations()`, with user annotations taking precedence for non-reserved keys.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4491 - "add some annotations"

**Use Cases**:
- Monitoring system integration (e.g., Prometheus annotations)
- Security policy markers (e.g., AppArmor, SELinux profiles)
- Custom tooling metadata

### Custom Labels Management

Labels enable pod selection and organization through custom labeling schemes:

**controllers/activemqartemis_reconciler.go**: Labels from api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.labels are controllers/activemqartemis_reconciler.go via pkg/resources/pods/pod.go::MakePodTemplateSpec().

**controllers/activemqartemis_controller.go**: The controllers/activemqartemis_controller.go::validateReservedLabels() function controllers/activemqartemis_controller.go like:
- pkg/utils/selectors/label.go::ActiveMQArtemis: Used for pkg/resources/statefulsets/statefulset.go
- pkg/utils/selectors/label.go::application: Used for pkg/utils/selectors/label.go

**Label Validation** (controllers/activemqartemis_controller.go):
- controllers/activemqartemis_controller.go for reserved labels
- **Fails validation** if user attempts to override operator-managed labels
- controllers/activemqartemis_controller.go preventing selector conflicts

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4595 - "passing in 2 labels"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L4636 - "passing in 8 labels"
- controllers/activemqartemis_controller_unit_test.go::activemqartemis_controller_unit_test.go#L41 - "TestValidate" (reserved label detection)

**Configuration Example**:
```yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: custom-metadata-broker
spec:
  deploymentPlan:
    labels:
      team: messaging
      environment: production
      cost-center: "12345"
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/port: "8161"
      custom-tool/version: "2.0"
```

### Operator-Managed Labels

The operator sets specific labels for resource selection and organization:

**[StatefulSet Labels](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/selectors/label.go#L37-38)**: Applied to all broker pods:
- `ActiveMQArtemis: {cr-name}` - pkg/utils/selectors/label.go (pkg/utils/selectors/label.go)
- `application: {cr-name}-app` - pkg/utils/selectors/label.go (pkg/utils/selectors/label.go)

**pkg/resources/services/service.go**: Services use pkg/resources/services/service.go for pkg/resources/services/service.go (sets `Spec.Selector`).

**controllers/activemqartemis_reconciler.go**: PodDisruptionBudget controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go to automatically target broker pods using `ActiveMQArtemis` label.

---

## 21. Storage and Persistence Architecture

> **Note**: For user-facing storage configuration, see the [operator help documentation](../help/operator.md#persistence). This section provides comprehensive architectural coverage; for storage validation see [Section 10](#4-storage-validation), for PVC retention in recovery scenarios see [Section 16](#2-pvc-persistence).

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L8020 - Storage Configuration (2 scenarios)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3411 - PVC Management (2 scenarios)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6799 - Persistence Toggle (2 scenarios)

The operator manages PersistentVolumeClaim lifecycle, storage class selection, and supports dynamic persistence toggling with PVC retention for data safety.

### PVC Creation and Management

**controllers/activemqartemis_reconciler.go**: When api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.persistenceEnabled: true, the operator controllers/activemqartemis_reconciler.go.

**Storage Configuration**:

1. **controllers/activemqartemis_reconciler.go**: Uses api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.storage.storageClassName if controllers/activemqartemis_reconciler.go, controllers/activemqartemis_reconciler.go
2. **controllers/activemqartemis_reconciler.go**: Sets size from api/v1beta1/activemqartemis_types.go::spec.deploymentPlan.storage.size (default: 2Gi), controllers/activemqartemis_reconciler.go
3. **controllers/activemqartemis_controller.go**: controllers/activemqartemis_controller.go via Kubernetes quantity parser

**controllers/activemqartemis_reconciler.go**: PVCs controllers/activemqartemis_reconciler.go which StatefulSet expands to `{name}-{ordinal}` pattern.

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L8021 - "checking storageClassName is configured"
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L8084 - "checking storage size mal configured"

### PVC Retention and Persistence

PVCs are intentionally retained when brokers are deleted to prevent data loss:

**controllers/activemqartemis_reconciler.go**: Unlike other resources, controllers/activemqartemis_reconciler.go (notice no `SetOwnerReferences` call), preventing garbage collection by Kubernetes.

**Persistence Behavior**:
1. **Broker Deletion**: PVCs remain in cluster (StatefulSet manages lifecycle, not owner refs)
2. **Broker Recreation**: New broker controllers/activemqartemis_reconciler.go via controllers/activemqartemis_reconciler.go with data intact
3. **Manual Cleanup**: Users must manually delete PVCs when data is no longer needed

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3412 - "deploy, verify, undeploy, verify, redeploy, verify" (PVC persistence across broker lifecycle)

### PVC Adoption for Upgrades

The operator can adopt pre-existing PVCs that lack owner references:

**controllers/activemqartemis_reconciler.go**: Similar to secret adoption, the operator controllers/activemqartemis_reconciler.go and controllers/activemqartemis_reconciler.go.

**🧪 Tested in**: controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L3522 - "faking a broker deployment with owned pvc" (upgrade scenario)

### Dynamic Persistence Toggle

Persistence can be toggled on existing brokers:

**controllers/activemqartemis_reconciler.go**: When persistence is controllers/activemqartemis_reconciler.go:

1. **controllers/activemqartemis_reconciler.go**: Operator controllers/activemqartemis_reconciler.go for the pod template
2. **controllers/activemqartemis_reconciler.go**: Operator controllers/activemqartemis_reconciler.go based on storage configuration
3. **controllers/activemqartemis_reconciler.go**: Operator controllers/activemqartemis_reconciler.go
4. **StatefulSet Controller Behavior**: Kubernetes StatefulSet controller detects spec change and performs rolling update, creating PVCs and mounting them in new pods (see [Section 7: StatefulSet Creation and Updates](#statefulset-creation-and-updates) for details)

**controllers/activemqartemis_reconciler.go**: The operator ensures configuration controllers/activemqartemis_reconciler.go.

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6800 - "Expect ok redeploy" (enable persistence)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L6859 - "check reconcole loop" (convergence verification)

---

## 22. Error Handling and Status Management

> **Note**: For user-facing error troubleshooting, see the [operator help documentation](../help/operator.md#troubleshooting). This section provides comprehensive status management architecture; for validation-specific error handling see [Section 10: Validation Error Handling](#validation-error-handling).

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L493 - Error Handling (2 scenarios)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5405 - Status Tracking

The operator implements comprehensive error detection, status condition management, and pod status tracking to provide clear operational visibility.

### Error Detection and Reporting

The operator detects and reports errors at multiple stages:

**controllers/activemqartemis_controller.go**: During controllers/activemqartemis_controller.go::ProcessStatus(), the reconciler controllers/activemqartemis_controller.go.

**Error Condition Types**:

1. **DeployedCondition** (api/v1beta1/activemqartemis_types.go::DeployedConditionType): Indicates resource creation success/failure
   - pkg/utils/common/common.go with reason `DeployedConditionCrudKindErrorReason`
   - pkg/utils/common/common.go

2. **ValidCondition** (api/v1beta1/activemqartemis_types.go::ValidConditionType): Indicates CR validation status
   - See [Section 10: Validation Architecture](#10-validation-architecture) for details
   - controllers/activemqartemis_controller.go

3. **ConfigAppliedCondition** (api/v1beta1/activemqartemis_types.go::ConfigAppliedConditionType): Indicates broker configuration application
   - controllers/activemqartemis_reconciler.go
   - controllers/activemqartemis_reconciler.go

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L494 - "two acceptors with names too long for kube" (name length error)
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L544 - "two acceptors with port clash" (port conflict error)

### Status Condition Management

The operator uses Kubernetes standard status conditions for state reporting:

**api/v1beta1/activemqartemis_types.go**:
```go
type Condition struct {
    Type               string      // e.g., "Valid", "Deployed", "Ready"
    Status             metav1.ConditionStatus  // "True", "False", "Unknown"
    Reason             string      // Machine-readable reason code
    Message            string      // Human-readable description
    ObservedGeneration int64       // CR generation when condition was set
    LastTransitionTime metav1.Time // When status changed
}
```

**pkg/utils/common/conditions.go**: The pkg/utils/common/conditions.go::SetReadyCondition() function:
1. [Checks all other conditions](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/conditions.go#L35-37)
2. [Sets Ready=False if any are False](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/conditions.go#L40-43)
3. [Sets Ready=True only if all are True](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/conditions.go#L33-34)
4. pkg/utils/common/conditions.go

**🧪 Tested in**: pkg/utils/common/conditions_test.go::pkg/utils/common/conditions_test.go#L35 - SetReadyCondition (6 test scenarios)

### Pod Status Tracking

The operator tracks individual pod states in the CR status:

**api/v1beta1/activemqartemis_types.go**: The status includes `podStatus` field with:
- **Ready**: List of ready pod names
- **Stopped**: List of stopped pod names
- **Starting**: List of starting pod names with pkg/utils/common/common.go

**pkg/utils/common/common.go**: During pkg/utils/common/common.go::ProcessStatus():
1. pkg/utils/common/common.go via `updatePodStatus()`
2. pkg/utils/common/common.go via `getDeploymentCondition()`
3. pkg/utils/common/common.go
4. Called from controllers/activemqartemis_controller.go

**pkg/utils/common/common.go**: For pods in starting state, the pkg/utils/common/common.go::PodStartingStatusDigestMessage() function:
- [Extracts pod phase](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/common.go#L640-641)
- [Includes pod conditions](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/common.go#L647-659) (ImagePullBackOff, CrashLoopBackOff, etc.)
- [Formats human-readable diagnostic message](https://github.com/arkmq-org/activemq-artemis-operator/blob/4acadb95603c38b82d5d7f63fb538c37ed855662/pkg/utils/common/common.go#L636-664)

**🧪 Tested in**:
- controllers/activemqartemis_controller_test.go::activemqartemis_controller_test.go#L5406 - "expect pod desc"
- pkg/utils/common/conditions_test.go::pkg/utils/common/conditions_test.go#L279 - PodStartingStatusDigestMessage (7 scenarios)

**Status Example**:
```yaml
status:
  conditions:
  - type: Valid
    status: "True"
  - type: Deployed
    status: "True"
  - type: Ready
    status: "True"
  podStatus:
    ready:
    - broker-ss-0
    - broker-ss-1
    starting:
    - broker-ss-2: "ContainerCreating: Waiting for PVC"
```

