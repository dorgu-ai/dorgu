package importer

import (
	"fmt"
	"strings"
)

// render writes the ApplicationPersona YAML. It is hand-written rather than
// marshalled so the output keeps a readable field order and can carry comments
// explaining where each value came from.
func (p persona) render() string {
	var sb strings.Builder

	sb.WriteString("apiVersion: dorgu.io/v1\n")
	sb.WriteString("kind: ApplicationPersona\n")
	sb.WriteString("metadata:\n")
	fmt.Fprintf(&sb, "  name: %s\n", p.name)
	fmt.Fprintf(&sb, "  namespace: %s\n", p.namespace)
	sb.WriteString("  labels:\n")
	sb.WriteString("    app.kubernetes.io/managed-by: dorgu\n")
	fmt.Fprintf(&sb, "    app.kubernetes.io/name: %s\n", p.appName)
	if p.ownership.team != "" {
		fmt.Fprintf(&sb, "    dorgu.io/team: %s\n", p.ownership.team)
	}
	sb.WriteString("  annotations:\n")
	fmt.Fprintf(&sb, "    dorgu.io/imported-from: Deployment/%s\n", p.name)
	if p.image != "" {
		fmt.Fprintf(&sb, "    dorgu.io/imported-image: %s\n", p.image)
	}

	sb.WriteString("spec:\n")
	fmt.Fprintf(&sb, "  name: %s\n", p.appName)
	sb.WriteString("  version: \"1\"\n")
	fmt.Fprintf(&sb, "  type: %s\n", p.appType)
	sb.WriteString("  tier: standard\n")

	sb.WriteString("  technical:\n")
	fmt.Fprintf(&sb, "    description: %s\n", p.description)

	p.renderResources(&sb)
	p.renderScaling(&sb)
	p.renderHealth(&sb)
	p.renderNetworking(&sb)
	p.renderOwnership(&sb)

	return sb.String()
}

func (p persona) renderResources(sb *strings.Builder) {
	res := p.resources
	if res.requestsCPU == "" && res.requestsMemory == "" &&
		res.limitsCPU == "" && res.limitsMemory == "" {
		return
	}

	sb.WriteString("  resources:\n")
	if res.requestsCPU != "" || res.requestsMemory != "" {
		sb.WriteString("    requests:\n")
		if res.requestsCPU != "" {
			fmt.Fprintf(sb, "      cpu: \"%s\"\n", res.requestsCPU)
		}
		if res.requestsMemory != "" {
			fmt.Fprintf(sb, "      memory: \"%s\"\n", res.requestsMemory)
		}
	}
	if res.limitsCPU != "" || res.limitsMemory != "" {
		sb.WriteString("    limits:\n")
		if res.limitsCPU != "" {
			fmt.Fprintf(sb, "      cpu: \"%s\"\n", res.limitsCPU)
		}
		if res.limitsMemory != "" {
			fmt.Fprintf(sb, "      memory: \"%s\"\n", res.limitsMemory)
		}
	}
}

func (p persona) renderScaling(sb *strings.Builder) {
	sb.WriteString("  scaling:\n")
	fmt.Fprintf(sb, "    minReplicas: %d\n", p.minReplicas)
	fmt.Fprintf(sb, "    maxReplicas: %d\n", p.maxReplicas)
	sb.WriteString("    behavior: balanced\n")
}

func (p persona) renderHealth(sb *strings.Builder) {
	if p.health == nil {
		return
	}
	sb.WriteString("  health:\n")
	if p.health.livenessPath != "" {
		fmt.Fprintf(sb, "    livenessPath: %s\n", p.health.livenessPath)
	}
	if p.health.readinessPath != "" {
		fmt.Fprintf(sb, "    readinessPath: %s\n", p.health.readinessPath)
	}
	if p.health.port > 0 {
		fmt.Fprintf(sb, "    port: %d\n", p.health.port)
	}
	sb.WriteString("    startupGracePeriod: \"30s\"\n")
}

func (p persona) renderNetworking(sb *strings.Builder) {
	if len(p.ports) == 0 {
		return
	}
	sb.WriteString("  networking:\n")
	sb.WriteString("    ports:\n")
	for _, port := range p.ports {
		fmt.Fprintf(sb, "      - port: %d\n", port.number)
		fmt.Fprintf(sb, "        protocol: %s\n", port.protocol)
	}
}

func (p persona) renderOwnership(sb *strings.Builder) {
	own := p.ownership
	if own.team == "" && own.owner == "" && own.repository == "" {
		return
	}
	sb.WriteString("  ownership:\n")
	if own.team != "" {
		fmt.Fprintf(sb, "    team: %s\n", own.team)
	}
	if own.owner != "" {
		fmt.Fprintf(sb, "    owner: %s\n", own.owner)
	}
	if own.repository != "" {
		fmt.Fprintf(sb, "    repository: %s\n", own.repository)
	}
}
