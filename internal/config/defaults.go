package config

// applyDefaults fills in default values for missing config
func applyDefaults(cfg *Config) {
	if cfg.Version == "" {
		cfg.Version = "1"
	}

	if cfg.Naming.Pattern == "" {
		cfg.Naming.Pattern = "{app}"
	}
	cfg.Naming.DNSSafe = true

	if cfg.Resources.Defaults.Requests.CPU == "" {
		cfg.Resources.Defaults.Requests.CPU = "100m"
	}
	if cfg.Resources.Defaults.Requests.Memory == "" {
		cfg.Resources.Defaults.Requests.Memory = "128Mi"
	}
	if cfg.Resources.Defaults.Limits.CPU == "" {
		cfg.Resources.Defaults.Limits.CPU = "500m"
	}
	if cfg.Resources.Defaults.Limits.Memory == "" {
		cfg.Resources.Defaults.Limits.Memory = "512Mi"
	}

	// Default resource profiles
	if cfg.Resources.Profiles == nil {
		cfg.Resources.Profiles = map[string]ResourceSpec{
			"api": {
				Requests: ResourceValues{CPU: "100m", Memory: "256Mi"},
				Limits:   ResourceValues{CPU: "1000m", Memory: "1Gi"},
			},
			"worker": {
				Requests: ResourceValues{CPU: "500m", Memory: "512Mi"},
				Limits:   ResourceValues{CPU: "2000m", Memory: "2Gi"},
			},
			"web": {
				Requests: ResourceValues{CPU: "50m", Memory: "128Mi"},
				Limits:   ResourceValues{CPU: "500m", Memory: "512Mi"},
			},
		}
	}

	if cfg.Ingress.Class == "" {
		cfg.Ingress.Class = "nginx"
	}
	if cfg.Ingress.DomainSuffix == "" {
		cfg.Ingress.DomainSuffix = ".local"
	}

	if cfg.ArgoCD.Project == "" {
		cfg.ArgoCD.Project = "default"
	}
	if cfg.ArgoCD.Destination.Server == "" {
		cfg.ArgoCD.Destination.Server = "https://kubernetes.default.svc"
	}

	if cfg.CI.Provider == "" {
		cfg.CI.Provider = "github-actions"
	}

	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "openai"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "gpt-4"
	}
}
