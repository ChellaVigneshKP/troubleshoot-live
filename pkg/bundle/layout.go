package bundle

// Layout defines paths under which are particular resources stored, plus the
// per-format filenames and skip-lists needed to import a bundle.
type Layout interface {
	ClusterInfo() string
	ClusterResources() string
	PodLogs() string
	PreviousPodLogs() string
	ConfigMaps() string
	Secrets() string

	// Format-specific filenames (relative to the dirs above).
	ClusterVersionFile() string // under ClusterInfo()
	CRDsFile() string           // under ClusterResources()
	NamespacesFile() string     // under ClusterResources()

	// Import skip-lists, relative to ClusterResources().
	SkipResources() []string
	SkipDirs() []string

	// Spectro reports whether this is a Spectro kubectl-dump layout (YAML,
	// k8s/ prefix, normal-object configmaps/secrets) vs a native
	// troubleshoot.sh layout.
	Spectro() bool
}

// --- native troubleshoot.sh layout (default) ---

type defaultLayout struct{}

func (defaultLayout) ClusterInfo() string        { return "cluster-info" }
func (defaultLayout) ClusterResources() string   { return "cluster-resources" }
func (defaultLayout) PodLogs() string            { return "pod-logs" }
func (defaultLayout) PreviousPodLogs() string    { return "previous-pod-logs" }
func (defaultLayout) ConfigMaps() string         { return "configmaps" }
func (defaultLayout) Secrets() string            { return "secrets" }
func (defaultLayout) ClusterVersionFile() string { return "cluster_version.json" }
func (defaultLayout) CRDsFile() string           { return "custom-resource-definitions.json" }
func (defaultLayout) NamespacesFile() string     { return "namespaces.json" }
func (defaultLayout) Spectro() bool              { return false }

func (defaultLayout) SkipResources() []string {
	return []string{
		// crds are imported during a separate step
		"custom-resource-definitions.json",
		"pod-disruption-budgets-info.json",
		// api-resources from the discovery client
		"resources.json",
		// api-groups from the discovery client
		"groups.json",
		// namespaces are imported as first resource in a separate step
		"namespaces.json",
	}
}

func (defaultLayout) SkipDirs() []string {
	return []string{
		"auth-cani-list",
		"pod-disruption-budgets",
	}
}

// --- Spectro Cloud kubectl-dump layout (infra + edge) ---

type spectroLayout struct{}

func (spectroLayout) ClusterInfo() string        { return "k8s/cluster-info" }
func (spectroLayout) ClusterResources() string   { return "k8s/cluster-resources" }
func (spectroLayout) PodLogs() string            { return "k8s/pod-logs" }
func (spectroLayout) PreviousPodLogs() string    { return "k8s/previous-pod-logs" }
func (spectroLayout) ConfigMaps() string         { return "k8s/cluster-resources/configmaps" }
func (spectroLayout) Secrets() string            { return "k8s/cluster-resources/secrets" }
func (spectroLayout) ClusterVersionFile() string { return "cluster-version.yaml" }
func (spectroLayout) CRDsFile() string           { return "crds.yaml" }
func (spectroLayout) NamespacesFile() string     { return "namespaces.yaml" }
func (spectroLayout) Spectro() bool              { return true }

func (spectroLayout) SkipResources() []string {
	return []string{
		// imported during separate steps
		"crds.yaml",
		"namespaces.yaml",
		// webhooks reference services that won't exist; importing breaks other resources
		"mutatingwebhookconfigurations.yaml",
		"validatingwebhookconfigurations.yaml",
		// aggregated apiservices would collide with envtest's own
		"apiservices.yaml",
	}
}

func (spectroLayout) SkipDirs() []string {
	return []string{
		"apiservices",
		"auth-cani-list",
		"pod-disruption-budgets",
		"poddisruptionbudgets",
	}
}
