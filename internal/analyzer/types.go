package analyzer

// This file provides type aliases for backward compatibility.
// The actual types are defined in github.com/dorgu-ai/dorgu/internal/types

import "github.com/dorgu-ai/dorgu/internal/types"

// Type aliases for backward compatibility within the analyzer package
type (
	AppAnalysis        = types.AppAnalysis
	Port               = types.Port
	HealthCheck        = types.HealthCheck
	EnvVar             = types.EnvVar
	ScalingConfig      = types.ScalingConfig
	DockerfileAnalysis = types.DockerfileAnalysis
	ComposeAnalysis    = types.ComposeAnalysis
	ComposeService     = types.ComposeService
	PortMapping        = types.PortMapping
	CodeAnalysis       = types.CodeAnalysis
)
