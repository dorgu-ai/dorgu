# Dorgu ApplicationPersona CRD Specification

## Overview

The `ApplicationPersona` CRD is the cluster-resident representation of an application's identity, requirements, and operational context. It serves as:

1. **Source of truth** for what the application needs
2. **Validation anchor** for deployment checks
3. **Memory store** for incident history and learned patterns
4. **Communication bridge** between CLI agent and cluster operator

## CRD Definition

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: applicationpersonas.dorgu.io
spec:
  group: dorgu.io
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          required:
            - spec
          properties:
            spec:
              type: object
              required:
                - name
                - type
              properties:
                # Identity
                name:
                  type: string
                  description: Application name
                version:
                  type: string
                  description: Persona schema version
                
                # Classification
                type:
                  type: string
                  enum: [api, web, worker, cron, daemon]
                tier:
                  type: string
                  enum: [critical, standard, best-effort]
                  default: standard
                
                # Technical Profile
                technical:
                  type: object
                  properties:
                    language:
                      type: string
                    framework:
                      type: string
                    description:
                      type: string
                
                # Resource Constraints
                resources:
                  type: object
                  properties:
                    requests:
                      type: object
                      properties:
                        cpu:
                          type: string
                        memory:
                          type: string
                    limits:
                      type: object
                      properties:
                        cpu:
                          type: string
                        memory:
                          type: string
                    profile:
                      type: string
                      enum: [minimal, standard, compute-heavy, memory-heavy]
                
                # Scaling Behavior
                scaling:
                  type: object
                  properties:
                    minReplicas:
                      type: integer
                      minimum: 0
                    maxReplicas:
                      type: integer
                      minimum: 1
                    targetCPU:
                      type: integer
                      minimum: 1
                      maximum: 100
                    targetMemory:
                      type: integer
                      minimum: 1
                      maximum: 100
                    behavior:
                      type: string
                      enum: [conservative, balanced, aggressive]
                      default: balanced
                
                # Health Configuration
                health:
                  type: object
                  properties:
                    livenessPath:
                      type: string
                    readinessPath:
                      type: string
                    port:
                      type: integer
                    startupGracePeriod:
                      type: string
                      default: "30s"
                
                # Dependencies
                dependencies:
                  type: array
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                      type:
                        type: string
                        enum: [database, cache, queue, service, external]
                      required:
                        type: boolean
                        default: true
                      healthCheck:
                        type: string
                
                # Networking
                networking:
                  type: object
                  properties:
                    ports:
                      type: array
                      items:
                        type: object
                        properties:
                          port:
                            type: integer
                          protocol:
                            type: string
                            default: TCP
                          purpose:
                            type: string
                    ingress:
                      type: object
                      properties:
                        enabled:
                          type: boolean
                        host:
                          type: string
                        paths:
                          type: array
                          items:
                            type: string
                        tlsEnabled:
                          type: boolean
                
                # Ownership
                ownership:
                  type: object
                  properties:
                    team:
                      type: string
                    owner:
                      type: string
                    repository:
                      type: string
                    oncall:
                      type: string
                    runbook:
                      type: string
                
                # Policies
                policies:
                  type: object
                  properties:
                    security:
                      type: object
                      properties:
                        runAsNonRoot:
                          type: boolean
                          default: true
                        readOnlyRootFilesystem:
                          type: boolean
                          default: true
                        allowPrivilegeEscalation:
                          type: boolean
                          default: false
                    deployment:
                      type: object
                      properties:
                        strategy:
                          type: string
                          enum: [RollingUpdate, Recreate, BlueGreen, Canary]
                          default: RollingUpdate
                        maxSurge:
                          type: string
                          default: "25%"
                        maxUnavailable:
                          type: string
                          default: "25%"
                    maintenance:
                      type: object
                      properties:
                        window:
                          type: string
                        autoRestart:
                          type: boolean
                          default: false
            
            status:
              type: object
              properties:
                # Current State
                phase:
                  type: string
                  enum: [Pending, Active, Degraded, Failed]
                lastUpdated:
                  type: string
                  format: date-time
                
                # Deployment Tracking
                deployments:
                  type: object
                  properties:
                    current:
                      type: string
                    lastSuccessful:
                      type: string
                    lastFailed:
                      type: string
                    history:
                      type: array
                      items:
                        type: object
                        properties:
                          version:
                            type: string
                          timestamp:
                            type: string
                          status:
                            type: string
                          triggeredBy:
                            type: string
                
                # Health Status
                health:
                  type: object
                  properties:
                    status:
                      type: string
                      enum: [Healthy, Degraded, Unhealthy, Unknown]
                    lastCheck:
                      type: string
                    message:
                      type: string
                
                # Validation Results
                validation:
                  type: object
                  properties:
                    passed:
                      type: boolean
                    lastChecked:
                      type: string
                    issues:
                      type: array
                      items:
                        type: object
                        properties:
                          severity:
                            type: string
                            enum: [error, warning, info]
                          field:
                            type: string
                          message:
                            type: string
                          suggestion:
                            type: string
                
                # Learned Patterns (Soul Memory)
                learned:
                  type: object
                  properties:
                    resourceBaseline:
                      type: object
                      properties:
                        avgCPU:
                          type: string
                        avgMemory:
                          type: string
                        peakCPU:
                          type: string
                        peakMemory:
                          type: string
                    incidentCount:
                      type: integer
                    lastIncident:
                      type: string
                    patterns:
                      type: array
                      items:
                        type: object
                        properties:
                          type:
                            type: string
                          description:
                            type: string
                          confidence:
                            type: number
                
                # Recommendations
                recommendations:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        enum: [resource, scaling, security, cost, performance]
                      priority:
                        type: string
                        enum: [high, medium, low]
                      message:
                        type: string
                      action:
                        type: string
      
      subresources:
        status: {}
      
      additionalPrinterColumns:
        - name: Type
          type: string
          jsonPath: .spec.type
        - name: Tier
          type: string
          jsonPath: .spec.tier
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Health
          type: string
          jsonPath: .status.health.status
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
  
  scope: Namespaced
  names:
    plural: applicationpersonas
    singular: applicationpersona
    kind: ApplicationPersona
    shortNames:
      - persona
      - ap
```

## Example ApplicationPersona

```yaml
apiVersion: dorgu.io/v1
kind: ApplicationPersona
metadata:
  name: order-service
  namespace: commerce
  labels:
    app.kubernetes.io/managed-by: dorgu
    dorgu.io/team: commerce-backend
spec:
  name: order-service
  version: "1"
  type: api
  tier: critical
  
  technical:
    language: java
    framework: spring
    description: |
      Order processing and management API for the e-commerce platform.
      Handles order creation, updates, cancellation, and fulfillment workflow.
  
  resources:
    requests:
      cpu: "500m"
      memory: "1Gi"
    limits:
      cpu: "2000m"
      memory: "2Gi"
    profile: standard
  
  scaling:
    minReplicas: 5
    maxReplicas: 50
    targetCPU: 65
    behavior: balanced
  
  health:
    livenessPath: /actuator/health/liveness
    readinessPath: /actuator/health/readiness
    port: 8000
    startupGracePeriod: "60s"
  
  dependencies:
    - name: mysql
      type: database
      required: true
      healthCheck: "SELECT 1"
    - name: redis
      type: cache
      required: true
    - name: inventory-service
      type: service
      required: true
    - name: stripe-api
      type: external
      required: true
  
  networking:
    ports:
      - port: 8000
        protocol: TCP
        purpose: HTTP API
    ingress:
      enabled: true
      host: orders-api.company.com
      paths:
        - /api/v1/orders
        - /api/v1/checkout
      tlsEnabled: true
  
  ownership:
    team: commerce-backend
    owner: orders-team@company.com
    repository: https://github.com/company/order-service
    oncall: commerce-oncall@company.com
    runbook: https://wiki.company.com/runbooks/order-service
  
  policies:
    security:
      runAsNonRoot: true
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
    deployment:
      strategy: RollingUpdate
      maxSurge: "25%"
      maxUnavailable: "0"
    maintenance:
      window: "Never - 24/7 critical service"
      autoRestart: false

status:
  phase: Active
  lastUpdated: "2026-01-15T10:30:00Z"
  
  deployments:
    current: "v2.3.1"
    lastSuccessful: "v2.3.1"
    history:
      - version: "v2.3.1"
        timestamp: "2026-01-15T10:30:00Z"
        status: successful
        triggeredBy: ci/github-actions
      - version: "v2.3.0"
        timestamp: "2026-01-14T14:00:00Z"
        status: successful
        triggeredBy: ci/github-actions
  
  health:
    status: Healthy
    lastCheck: "2026-01-15T11:00:00Z"
    message: "All health checks passing"
  
  validation:
    passed: true
    lastChecked: "2026-01-15T10:30:00Z"
    issues: []
  
  learned:
    resourceBaseline:
      avgCPU: "350m"
      avgMemory: "800Mi"
      peakCPU: "1500m"
      peakMemory: "1.5Gi"
    incidentCount: 2
    lastIncident: "2026-01-10T03:00:00Z"
    patterns:
      - type: traffic-spike
        description: "Traffic increases 3x during 12:00-14:00 UTC"
        confidence: 0.92
      - type: memory-growth
        description: "Gradual memory increase over 7 days, restart helps"
        confidence: 0.78
  
  recommendations:
    - type: scaling
      priority: medium
      message: "Consider increasing minReplicas to 7 based on traffic patterns"
      action: "spec.scaling.minReplicas=7"
    - type: resource
      priority: low
      message: "Memory baseline suggests limits can be reduced to 1.8Gi"
      action: "spec.resources.limits.memory=1800Mi"
```

## Operator Validation Behavior

When a Deployment is created/updated, the operator:

1. **Finds matching Persona** by app name or label
2. **Validates constraints**:
   - Resource requests/limits match persona constraints
   - Replicas within min/max bounds
   - Health probes configured correctly
   - Security context matches policy
3. **Reports issues** via admission webhook (warn) or status update
4. **Updates persona status** with deployment info
5. **Learns patterns** over time (resource usage, incidents)

## CLI Integration

The CLI will have a new command:

```bash
# Generate and apply persona to cluster
dorgu persona apply ./app-path --namespace commerce

# View persona status
dorgu persona status order-service -n commerce

# Sync persona from cluster to local
dorgu persona pull order-service -n commerce > .dorgu.yaml
```

## Future: Incident Memory CRD

```yaml
apiVersion: dorgu.io/v1
kind: IncidentMemory
metadata:
  name: order-service-2026-01-10
  namespace: commerce
spec:
  application: order-service
  timestamp: "2026-01-10T03:00:00Z"
  severity: high
  symptoms:
    - "5xx errors spiked to 15%"
    - "Response latency > 5s"
    - "Pod restarts detected"
  rootCause: "MySQL connection pool exhausted"
  resolution: "Increased pool size from 20 to 50"
  preventiveMeasures:
    - "Added connection pool monitoring"
    - "Updated persona with pool size requirement"
  relatedPersonas:
    - order-service
    - mysql-primary
```

This creates a living memory of incidents that the operator can reference for pattern matching and proactive alerts.
