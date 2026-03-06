package discovery

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ContainerRef holds metadata parsed from a K8s container log filename.
type ContainerRef struct {
	Pod       string
	Namespace string
	Container string
	ID        string // container runtime ID (64-char hex or truncated)
}

// ParseFilename extracts container metadata from a K8s log filename.
// Expected format: {pod}_{namespace}_{container}-{id}.log
// The filename may be an absolute path; only the base name is parsed.
func ParseFilename(path string) (ContainerRef, error) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".log") {
		return ContainerRef{}, fmt.Errorf("discovery: not a .log file: %q", base)
	}
	base = strings.TrimSuffix(base, ".log")

	// Split from the right: the last segment after '-' is the container ID,
	// but the container name + ID is the third underscore-separated field.
	// Format: {pod}_{namespace}_{container}-{id}
	//
	// Pod names can contain hyphens AND underscores (rare but valid).
	// Namespace cannot contain underscores.
	// Container name cannot contain underscores.
	// So we split on underscore from the right to find namespace and container-id,
	// and everything before is the pod name.

	// Find the last underscore — separates container-id from the rest.
	lastUnderscore := strings.LastIndex(base, "_")
	if lastUnderscore < 0 {
		return ContainerRef{}, fmt.Errorf("discovery: no underscore separator in %q", base)
	}
	containerAndID := base[lastUnderscore+1:]
	rest := base[:lastUnderscore]

	// Find the second-to-last underscore — separates namespace from pod.
	secondLastUnderscore := strings.LastIndex(rest, "_")
	if secondLastUnderscore < 0 {
		return ContainerRef{}, fmt.Errorf("discovery: missing namespace separator in %q", base)
	}
	namespace := rest[secondLastUnderscore+1:]
	pod := rest[:secondLastUnderscore]

	if pod == "" || namespace == "" || containerAndID == "" {
		return ContainerRef{}, fmt.Errorf("discovery: empty field in %q", base)
	}

	// Split container-id on the last hyphen to get container name and ID.
	lastHyphen := strings.LastIndex(containerAndID, "-")
	if lastHyphen < 0 {
		return ContainerRef{}, fmt.Errorf("discovery: no container ID separator in %q", containerAndID)
	}
	container := containerAndID[:lastHyphen]
	id := containerAndID[lastHyphen+1:]

	if container == "" || id == "" {
		return ContainerRef{}, fmt.Errorf("discovery: empty container or ID in %q", base)
	}

	return ContainerRef{
		Pod:       pod,
		Namespace: namespace,
		Container: container,
		ID:        id,
	}, nil
}
