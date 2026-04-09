package setup

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ComponentInfo holds the access details for a single installed component.
// It is the user-facing record returned by `dorgu cluster info` and consumed
// by the post-install Quick Access summary.
type ComponentInfo struct {
	ID             ComponentID   `json:"id"`
	DisplayName    string        `json:"displayName"`
	Namespace      string        `json:"namespace"`
	Installed      bool          `json:"installed"`
	ServiceName    string        `json:"serviceName,omitempty"`
	ServiceType    string        `json:"serviceType,omitempty"` // ClusterIP, LoadBalancer, NodePort
	ClusterIP      string        `json:"clusterIP,omitempty"`
	ExternalIP     string        `json:"externalIP,omitempty"` // LoadBalancer IP or hostname
	Ports          []ServicePort `json:"ports,omitempty"`
	WebUIPort      int           `json:"webUIPort,omitempty"`
	WebUIURL       string        `json:"webUIURL,omitempty"`
	PortForwardCmd string        `json:"portForwardCmd,omitempty"`
	CredentialCmd  string        `json:"credentialCmd,omitempty"`
	Username       string        `json:"username,omitempty"`
	Notes          string        `json:"notes,omitempty"`
	ServiceError   string        `json:"serviceError,omitempty"` // populated when kubectl get svc fails
}

// ServicePort describes a single port exposed by a Kubernetes service.
type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Port       int    `json:"port"`
	TargetPort string `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
}

// k8sService is a minimal subset of the Service JSON we parse.
type k8sService struct {
	Spec struct {
		Type      string `json:"type"`
		ClusterIP string `json:"clusterIP"`
		Ports     []struct {
			Name       string `json:"name"`
			Port       int    `json:"port"`
			TargetPort any    `json:"targetPort"`
			Protocol   string `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// GetComponentInfo queries the cluster for access details of a single component.
// It never returns an error: if kubectl fails or the service is missing the
// returned ComponentInfo records the failure in ServiceError so the caller can
// still display whatever static metadata is available.
func GetComponentInfo(ex Executor, c ComponentConfig) ComponentInfo {
	info := ComponentInfo{
		ID:          c.ID,
		DisplayName: c.DisplayName,
		Namespace:   c.Namespace,
		Installed:   true,
	}

	svcName := c.HelmReleaseName
	if c.Access != nil && c.Access.ServiceName != "" {
		svcName = c.Access.ServiceName
	}
	info.ServiceName = svcName

	// 1. Query service by name; on failure try label-based discovery.
	out, err := ex.Run("kubectl", "get", "svc", svcName, "-n", c.Namespace, "-o", "json")
	if err != nil {
		labelOut, labelErr := ex.Run("kubectl", "get", "svc", "-n", c.Namespace,
			"-l", fmt.Sprintf("app.kubernetes.io/instance=%s", c.HelmReleaseName),
			"-o", "json")
		if labelErr == nil {
			var svcList struct {
				Items []json.RawMessage `json:"items"`
			}
			if json.Unmarshal([]byte(labelOut), &svcList) == nil && len(svcList.Items) > 0 {
				out = string(svcList.Items[0])
				err = nil
			}
		}
		if err != nil {
			// Set a clean, actionable message instead of raw API server text.
			if c.Access == nil || c.Access.WebUIPort == 0 {
				info.Notes = "No user-facing service (operator/controller only)"
			} else {
				info.ServiceError = fmt.Sprintf("service %q not found in namespace %s", svcName, c.Namespace)
			}
		}
	}

	if err == nil {
		var svc k8sService
		if jerr := json.Unmarshal([]byte(out), &svc); jerr == nil {
			info.ServiceType = svc.Spec.Type
			info.ClusterIP = svc.Spec.ClusterIP
			for _, p := range svc.Spec.Ports {
				info.Ports = append(info.Ports, ServicePort{
					Name:       p.Name,
					Port:       p.Port,
					TargetPort: targetPortString(p.TargetPort),
					Protocol:   p.Protocol,
				})
			}
			for _, ing := range svc.Status.LoadBalancer.Ingress {
				if ing.IP != "" {
					info.ExternalIP = ing.IP
					break
				}
				if ing.Hostname != "" {
					info.ExternalIP = ing.Hostname
					break
				}
			}
		}
	}

	// 2. Build port-forward command and access URL
	if c.Access != nil && c.Access.WebUIPort > 0 {
		localPort := suggestLocalPort(c.Access.WebUIPort)
		info.WebUIPort = c.Access.WebUIPort
		info.PortForwardCmd = fmt.Sprintf(
			"kubectl port-forward -n %s svc/%s %d:%d",
			c.Namespace, svcName, localPort, c.Access.WebUIPort,
		)
		scheme := "http"
		if c.Access.WebUIPort == 443 || c.Access.WebUIPort == 8443 {
			scheme = "https"
		}
		info.WebUIURL = fmt.Sprintf("%s://localhost:%d", scheme, localPort)
	}

	// 3. Build credential retrieval command
	if c.Access != nil && c.Access.DefaultCredentials != nil {
		cred := c.Access.DefaultCredentials
		ns := c.Namespace
		if cred.Namespace != "" {
			ns = cred.Namespace
		}
		info.Username = cred.UsernameValue
		if cred.UsernameKey != "" {
			info.Username = fmt.Sprintf("(from secret %s key %s)", cred.SecretName, cred.UsernameKey)
		}
		info.CredentialCmd = fmt.Sprintf(
			"kubectl get secret %s -n %s -o jsonpath='{.data.%s}' | base64 -d",
			cred.SecretName, ns, cred.PasswordKey,
		)
		info.Notes = cred.Notes
	}

	return info
}

// GetInstalledComponentsInfo returns access info for all installed blessed
// stack components, derived from the dorgu.io/setup-stack annotation on the
// named ClusterPersona.
func GetInstalledComponentsInfo(ex Executor, clusterPersonaName string) ([]ComponentInfo, error) {
	if clusterPersonaName == "" {
		return nil, fmt.Errorf("ClusterPersona name is required")
	}

	stackAnnotation, err := ex.Run("kubectl", "get", "clusterpersona", clusterPersonaName,
		"-o", `jsonpath={.metadata.annotations.dorgu\.io/setup-stack}`)
	if err != nil {
		return nil, fmt.Errorf("failed to read ClusterPersona %s: %w\n%s", clusterPersonaName, err, stackAnnotation)
	}

	installedIDs := parseInstalledIDs(stackAnnotation)
	if len(installedIDs) == 0 {
		// Try ArgoCD Application discovery as fallback (GitOps driver installs).
		argoApps, fallbackErr := discoverFromArgoCD(ex, clusterPersonaName)
		if fallbackErr == nil && len(argoApps) > 0 {
			return argoApps, nil
		}
		return nil, fmt.Errorf(
			"ClusterPersona %q has no installed components recorded yet.\n"+
				"If you used GitOps driver, components will appear after ArgoCD syncs.\n"+
				"Run 'dorgu cluster status --addons' to check addon discovery.",
			clusterPersonaName)
	}

	stack := DefaultStack()
	var infos []ComponentInfo
	for _, c := range stack.Components() {
		if installedIDs[c.ID] {
			infos = append(infos, GetComponentInfo(ex, c))
		}
	}
	return infos, nil
}

// parseInstalledIDs converts a comma-separated list of component IDs into a
// set keyed by ComponentID.
func parseInstalledIDs(annotation string) map[ComponentID]bool {
	out := map[ComponentID]bool{}
	for raw := range strings.SplitSeq(annotation, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		out[ComponentID(id)] = true
	}
	return out
}

// suggestLocalPort returns a reasonable local port for port-forwarding.
// Privileged ports are mapped to common local equivalents; non-privileged
// ports are reused as-is so users see familiar numbers.
func suggestLocalPort(targetPort int) int {
	if targetPort >= 1024 {
		return targetPort
	}
	switch targetPort {
	case 443:
		return 8443
	case 80:
		return 8080
	default:
		return 8000 + targetPort
	}
}

// discoverFromArgoCD queries ArgoCD Applications labeled for a specific
// ClusterPersona and maps them to ComponentInfo records.
// Returns nil, nil when ArgoCD is not installed or no matching Applications exist.
func discoverFromArgoCD(ex Executor, clusterPersonaName string) ([]ComponentInfo, error) {
	out, err := ex.Run("kubectl", "get", "applications.argoproj.io",
		"-l", "dorgu.io/cluster-persona="+clusterPersonaName,
		"-A", "-o", "json")
	if err != nil {
		return nil, err
	}

	var appList struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Health struct {
					Status string `json:"status"`
				} `json:"health"`
				Sync struct {
					Status string `json:"status"`
				} `json:"sync"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &appList); err != nil {
		return nil, err
	}

	allComponents := DefaultStack().Components()
	componentMap := make(map[ComponentID]ComponentConfig, len(allComponents))
	for _, c := range allComponents {
		componentMap[c.ID] = c
	}

	var infos []ComponentInfo
	for _, app := range appList.Items {
		for id, c := range componentMap {
			if strings.Contains(app.Metadata.Name, string(id)) {
				info := GetComponentInfo(ex, c)
				info.Notes = fmt.Sprintf("Managed by ArgoCD (sync: %s, health: %s)",
					app.Status.Sync.Status, app.Status.Health.Status)
				infos = append(infos, info)
				break
			}
		}
	}

	return infos, nil
}

// targetPortString normalises the targetPort field which may be an int or string.
func targetPortString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%d", int(t))
	case int:
		return fmt.Sprintf("%d", t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
