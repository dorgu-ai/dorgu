# Dorgu: AI-Powered Kubernetes Application Onboarding

## The Problem

**Platform engineering teams are drowning in repetitive onboarding work.**

Every new application that needs to run on Kubernetes requires:
- Writing Deployment, Service, Ingress, HPA manifests
- Configuring CI/CD pipelines (ArgoCD, Tekton, GitHub Actions)
- Setting up monitoring, logging, and alerting
- Ensuring compliance with organizational standards
- Documenting what the application does and who owns it

**This takes 2-4 hours per application.** For a growing startup deploying 5-10 new services per month, that's 20-40 hours of senior engineering time spent on copy-paste-modify work.

### The Pain Points

1. **Platform teams are bottlenecks** — Every team waits for platform engineers to onboard their app
2. **Standards drift** — Without automation, naming conventions, resource limits, and security policies become inconsistent
3. **Tribal knowledge** — "What does this service do?" lives in someone's head, not in documentation
4. **Context switching** — Platform engineers interrupt deep work to handle routine onboarding requests
5. **Slow iteration** — Developers can't self-serve; they wait days for platform team availability

---

## The Solution

**Dorgu** is an AI-powered CLI that understands your containerized application and generates production-ready Kubernetes resources in seconds.

```bash
$ dorgu generate ./my-app

Analyzing application...
✓ Detected: Node.js Express API
✓ Found: Port 3000, health endpoint /health
✓ Identified: PostgreSQL dependency, Redis cache
✓ Inferred: Memory ~256Mi, CPU ~100m baseline

Generating resources...
✓ k8s/deployment.yaml
✓ k8s/service.yaml  
✓ k8s/ingress.yaml
✓ k8s/hpa.yaml
✓ argocd/application.yaml
✓ .github/workflows/deploy.yaml
✓ PERSONA.md

Done. Review manifests in ./k8s/
```

### How It Works

1. **Point it at your app** — Dockerfile, docker-compose, or existing deployment
2. **AI analyzes** — Code structure, dependencies, ports, env vars, resource patterns
3. **Generates everything** — K8s manifests, CI/CD pipelines, application documentation
4. **Applies your standards** — Naming conventions, resource defaults, required labels, security policies
5. **Creates the persona** — A living document describing what this application does

### What Makes Dorgu Different

| Traditional Approach | Dorgu |
|---------------------|-------|
| Copy-paste from templates | Intelligent generation based on app analysis |
| Manual resource sizing | Inferred from application characteristics |
| Standards enforced by review | Standards baked into generation |
| Documentation written separately | Persona generated automatically |
| Platform team bottleneck | Developer self-service |

---

## Target Customer

**Primary:** Platform engineering teams at Series A-C startups (30-200 engineers)

- Running Kubernetes in production
- Onboarding 3-10+ new services per month
- Want to enforce standards without slowing teams down
- Value developer self-service

**Characteristics:**
- Using ArgoCD, Flux, or similar GitOps tooling
- Have organizational standards they want to enforce
- Platform team of 2-5 engineers supporting 20-100+ developers

---

## Business Model

### Free Tier (Open Source CLI)
- Generate manifests for unlimited apps
- Basic org standards configuration
- Community support

### Pro Tier ($49/month per seat)
- Advanced org standards (security policies, compliance templates)
- Team sharing of configurations
- Priority support
- Usage analytics

### Enterprise (Custom pricing)
- On-premise deployment
- SSO/SAML integration
- Audit logging
- Custom integrations
- Dedicated support

---

## North Star Goal

> **Every application deployed to Kubernetes has a living, queryable persona that describes what it does, how it works, and how to interact with it—created automatically, not manually.**

This isn't just about generating YAML files. It's about building an **understanding layer** for your infrastructure.

### The Vision (18-24 months)

**Phase 1 (Now):** Generate manifests → Save platform team time

**Phase 2 (6 months):** Application personas → Queryable knowledge base of your services

**Phase 3 (12 months):** Cross-service understanding → "Show me everything that depends on the payments API"

**Phase 4 (18 months):** Intelligent operations → Incident context, optimization suggestions, autonomous remediation with approval gates

The manifest generation is the **wedge**. The application understanding layer is the **platform**.

---

## Traction Goals

| Milestone | Timeline | Metric |
|-----------|----------|--------|
| Launch CLI | Month 3 | Public release |
| Early adopters | Month 4 | 100 GitHub stars, 20 active users |
| Design partners | Month 5 | 5 companies using in staging |
| First revenue | Month 6 | 3 paying customers |
| Product-market fit signal | Month 9 | 10 paying customers, <5% monthly churn |

---

## Why Now?

1. **LLMs can now reason about code** — Two years ago, this was impossible. Today, Claude and GPT-4 can analyze codebases and generate valid Kubernetes manifests.

2. **Platform engineering is a recognized discipline** — Teams exist, budgets exist, the pain is acknowledged.

3. **The giants aren't here** — Datadog, Dynatrace focus on post-deployment monitoring. ArgoCD, Flux focus on deployment mechanics. Nobody owns the "understanding" layer.

4. **Developer experience is a priority** — Companies are investing in IDPs (Backstage, Port). Dorgu complements these investments.

---

## The Ask

**For design partners:** 
- 30-minute call to understand your onboarding workflow
- Free access to Dorgu Pro for 6 months
- Shape the product roadmap with your feedback

**For investors:**
- Pre-seed: $500K to build the core product and reach 10 paying customers
- Use of funds: 12 months runway, 1-2 additional engineers

---

## Team

[Your background here]
- Platform engineering at [Company] — Built GitOps CI/CD with ArgoCD, Tekton, Backstage
- Managed logging infrastructure at scale
- Deep understanding of the platform team pain points from the inside

---

## Contact

[Email]
[LinkedIn]
[GitHub: github.com/dorgu-ai/dorgu]
