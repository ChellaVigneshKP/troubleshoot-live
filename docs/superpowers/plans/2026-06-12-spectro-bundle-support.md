# Spectro Bundle Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `troubleshoot-live serve <bundle>` work end-to-end, with zero config, on Spectro Cloud sustaining-team support bundles (infra + edge), while native troubleshoot.sh bundles keep working.

**Architecture:** Auto-detect the Spectro `k8s/`-prefixed kubectl-dump layout in `bundle.New`, expose all layout-specific paths/filenames/skip-lists through the `Layout` interface, and branch the small number of format-sensitive call sites (resource loading, version detection, CRD/namespace paths, configmap/secret import, pod-log retrieval) on that layout. The critical enabler is teaching `LoadResourcesFromFile` to parse `kubectl get -o yaml` `kind: List` documents.

**Tech Stack:** Go, `sigs.k8s.io/controller-runtime` (envtest), `k8s.io/apimachinery` unstructured, `sigs.k8s.io/yaml`, `spf13/afero`, `spf13/cobra`.

**Module path:** `github.com/mhrabovcin/troubleshoot-live`

**Working dir:** `/Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live` (branch `spectro-bundle-support`).

**Note on testing:** Unit tests are out of scope this round (per request). Each task's verification is `go build ./...` (and `go vet ./...` where useful). End-to-end verification against the two real bundles is Task 8.

**Sample bundles (in `~/Downloads`):**
- Infra: `it-vault-dev-2026-06-09_17_59_26.tar.gz`
- Edge (with k8s): `edge-609e35423bb8e6209a04d5a6990a7b49-2026-05-18_14_13_41.tar.gz`

---

## File Structure

- `pkg/bundle/layout.go` — **Modify.** Extend `Layout` interface; add `spectroLayout`; add default values + skip lists.
- `pkg/bundle/bundle.go` — **Modify.** Store selected layout on `bundle`; auto-detect in `New`; host-only error.
- `pkg/bundle/resources.go` — **Modify.** YAML `kind: List` parsing in `LoadResourcesFromFile`.
- `pkg/bundle/service_ip_range.go` — **Modify.** Resolve `pods/kube-system` file by trying `.yaml` then `.json`.
- `pkg/envtest/detect_cluster_version.go` — **Modify.** Parse `kubectl version -o yaml` for Spectro layout.
- `pkg/envtest/setup.go` — **Modify.** `Prepare` honors an explicit version override.
- `pkg/envtest/options.go` — **Modify.** Add a version-override carrier (see Task 3).
- `cmd/serve.go` — **Modify.** Add `--kubernetes-version` flag; pass override.
- `pkg/importer/crds.go` — **Modify.** CRD path from layout.
- `pkg/importer/import.go` — **Modify.** Namespace path from layout; skip lists from layout; conditional CM/secret import.
- `pkg/proxy/logs.go` — **Modify.** Spectro pod-log candidate paths + helper.
- `README.md` — **Modify.** Document Spectro support + `--kubernetes-version`.

---

## Task 1: Layout interface + Spectro layout + auto-detection

**Files:**
- Modify: `pkg/bundle/layout.go`
- Modify: `pkg/bundle/bundle.go`

- [ ] **Step 1: Rewrite `pkg/bundle/layout.go`**

Replace the entire file with:

```go
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
func (defaultLayout) Spectro() bool               { return false }

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
func (spectroLayout) Spectro() bool               { return true }

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
```

- [ ] **Step 2: Update `bundle` struct + selection in `pkg/bundle/bundle.go`**

Change the struct and `Layout()` method (currently lines ~24-31):

```go
type bundle struct {
	afero.Fs
	layout Layout
}

func (b bundle) Layout() Layout {
	return b.layout
}
```

- [ ] **Step 3: Add layout detection helper + host-only error in `pkg/bundle/bundle.go`**

Add this `ErrNoKubernetesResources` var next to `ErrUnknownBundleFormat`:

```go
// ErrNoKubernetesResources is returned when a bundle has no k8s API data to serve.
var ErrNoKubernetesResources = fmt.Errorf(
	"bundle contains no Kubernetes resources; nothing to serve")
```

Add this helper at the end of the file (uses `afero` already imported):

```go
// detectLayout picks the Spectro layout when the k8s/ prefixed cluster-resources
// directory is present, otherwise the native troubleshoot.sh layout. It returns
// ErrNoKubernetesResources when neither cluster-resources directory exists.
func detectLayout(fs afero.Fs) (Layout, error) {
	if ok, _ := afero.DirExists(fs, spectroLayout{}.ClusterResources()); ok {
		return spectroLayout{}, nil
	}
	if ok, _ := afero.DirExists(fs, defaultLayout{}.ClusterResources()); ok {
		return defaultLayout{}, nil
	}
	return nil, ErrNoKubernetesResources
}
```

- [ ] **Step 4: Wire detection into `New` and `FromFs` in `pkg/bundle/bundle.go`**

In `New`, replace the archive return (currently `return FromFs(fromDir(filepath.Join(tmpDir, entries[0].Name()))), nil`) with:

```go
		fs := fromDir(filepath.Join(tmpDir, entries[0].Name()))
		layout, err := detectLayout(fs)
		if err != nil {
			return nil, err
		}
		return bundle{Fs: fs, layout: layout}, nil
```

In `New`, replace the directory return (currently `return FromFs(fromDir(absPath)), nil`) with:

```go
		fs := fromDir(absPath)
		layout, err := detectLayout(fs)
		if err != nil {
			return nil, err
		}
		return bundle{Fs: fs, layout: layout}, nil
```

Replace `FromFs` so callers/tests still work, defaulting to detection with a native fallback:

```go
// FromFs allows to create bundle from provided afero.Fs. The layout is
// auto-detected; when no cluster-resources directory is found it falls back to
// the native layout (useful for synthetic/in-memory filesystems in tests).
func FromFs(fs afero.Fs) Bundle {
	layout, err := detectLayout(fs)
	if err != nil {
		layout = defaultLayout{}
	}
	return bundle{Fs: fs, layout: layout}
}
```

- [ ] **Step 5: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly (no errors).

- [ ] **Step 6: Commit**

```bash
git add pkg/bundle/layout.go pkg/bundle/bundle.go
git commit -m "feat(bundle): auto-detect Spectro layout and extend Layout interface"
```

---

## Task 2: Parse kubectl `kind: List` YAML in resource loader

**Files:**
- Modify: `pkg/bundle/resources.go`

- [ ] **Step 1: Add the `sigs.k8s.io/yaml` import**

In `pkg/bundle/resources.go`, the import block currently includes `"k8s.io/apimachinery/pkg/util/yaml"`. Add a distinct alias for sigs yaml:

```go
	sigsyaml "sigs.k8s.io/yaml"
```

(Keep the existing `"k8s.io/apimachinery/pkg/util/yaml"` import — it is still used by other functions in the package. If `go build` reports it unused after Step 2, remove it.)

- [ ] **Step 2: Replace the YAML branch of `LoadResourcesFromFile`**

Currently the YAML branch is:

```go
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		items := []unstructured.Unstructured{}
		if err := yaml.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		list.Items = items
		return list, nil
	}
```

Replace it with (convert YAML to JSON, then reuse the JSON list parser which already
handles `kind: List`, bare arrays, and items-without-GVK, with correct int64 handling):

```go
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		jsonData, err := sigsyaml.YAMLToJSON(data)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON for %q: %w", path, err)
		}
		return parseJSONList(jsonData, path)
	}
```

- [ ] **Step 3: Build + tidy**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go mod tidy && go build ./...`
Expected: builds cleanly; `sigs.k8s.io/yaml` becomes a direct dependency in `go.mod`.

- [ ] **Step 4: Spot-check parsing against a real file (sanity, optional)**

Run a quick throwaway check that a real List file parses (uses the extracted infra bundle path produced by Task 8; skip if not yet extracted).

- [ ] **Step 5: Commit**

```bash
git add pkg/bundle/resources.go go.mod go.sum
git commit -m "fix(bundle): parse kubectl 'kind: List' YAML in LoadResourcesFromFile"
```

---

## Task 3: Spectro version detection + `--kubernetes-version` override

**Files:**
- Modify: `pkg/envtest/detect_cluster_version.go`
- Modify: `pkg/envtest/setup.go`
- Modify: `cmd/serve.go`

- [ ] **Step 1: Add Spectro version parsing in `pkg/envtest/detect_cluster_version.go`**

Add imports `"strings"` and `sigsyaml "sigs.k8s.io/yaml"` to the import block (keep `encoding/json`).

Replace the body of `DetectK8sVersion` with a layout-aware version that reads the
layout's `ClusterVersionFile()` and parses by format:

```go
// DetectK8sVersion attempts to load k8s server version from which the bundle
// was collected.
func DetectK8sVersion(b bundle.Bundle) (versions.Selector, error) {
	path := filepath.Join(b.Layout().ClusterInfo(), b.Layout().ClusterVersionFile())
	data, err := afero.ReadFile(b, path)
	if err != nil {
		return nil, err
	}

	if b.Layout().Spectro() {
		return detectSpectroVersion(data)
	}
	return detectNativeVersion(data)
}

func detectNativeVersion(data []byte) (versions.Selector, error) {
	i := &clusterInfo{}
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return selectorFromClusterInfo(i)
}

// spectroVersion matches the shape of `kubectl version -o yaml`.
type spectroVersion struct {
	ServerVersion struct {
		Major      string `json:"major"`
		Minor      string `json:"minor"`
		GitVersion string `json:"gitVersion"`
	} `json:"serverVersion"`
}

func detectSpectroVersion(data []byte) (versions.Selector, error) {
	jsonData, err := sigsyaml.YAMLToJSON(data)
	if err != nil {
		return nil, err
	}
	v := &spectroVersion{}
	if err := json.Unmarshal(jsonData, v); err != nil {
		return nil, err
	}
	i := &clusterInfo{}
	i.Info.Major = strings.TrimSuffix(v.ServerVersion.Major, "+")
	i.Info.Minor = strings.TrimSuffix(v.ServerVersion.Minor, "+")
	i.Info.GitVersion = v.ServerVersion.GitVersion
	return selectorFromClusterInfo(i)
}

func selectorFromClusterInfo(i *clusterInfo) (versions.Selector, error) {
	if sv, err := semver.NewVersion(i.VersionString); err == nil {
		return selectorFromSemver(sv), nil
	}
	if sv, err := semver.NewVersion(i.Info.GitVersion); err == nil {
		return selectorFromSemver(sv), nil
	}
	major, _ := strconv.Atoi(i.Info.Major)
	minor, _ := strconv.Atoi(i.Info.Minor)
	return versions.PatchSelector{
		Major: major,
		Minor: minor,
		Patch: versions.AnyPoint,
	}, nil
}
```

(Removes the old inline parsing; `clusterInfo`, `selectorFromSemver`, and the
imports `semver`, `strconv` remain in use.)

- [ ] **Step 2: Add `ParseVersionSelector` helper in `pkg/envtest/detect_cluster_version.go`**

Append:

```go
// ParseVersionSelector parses a user-provided version string (e.g. "1.31",
// "v1.31.2") into an envtest version selector.
func ParseVersionSelector(version string) (versions.Selector, error) {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid kubernetes version %q: %w", version, err)
	}
	return selectorFromSemver(sv), nil
}
```

Add `"fmt"` to the import block if not already present.

- [ ] **Step 3: Honor an override in `Prepare` (`pkg/envtest/setup.go`)**

Replace the start of `Prepare` (the `detectedK8sVersion` block) so an override
short-circuits detection. Change the signature to accept an override string:

```go
// Prepare creates k8s environment for the provided bundle. When versionOverride
// is non-empty it is used instead of detecting the version from the bundle.
func Prepare(ctx context.Context, b bundle.Bundle, versionOverride string, opts ...Option) (*Environment, error) {
	var detectedK8sVersion versions.Selector
	if versionOverride != "" {
		sel, err := ParseVersionSelector(versionOverride)
		if err != nil {
			return nil, err
		}
		detectedK8sVersion = sel
		log.Printf("Using overridden %q k8s version", versionOverride)
	} else {
		sel, err := DetectK8sVersion(b)
		if err != nil {
			return nil, fmt.Errorf("failed to detect k8s version: %w (use --kubernetes-version to set it manually)", err)
		}
		detectedK8sVersion = sel
		log.Printf("Detected %q k8s version", detectedK8sVersion)
	}

	versionSpec := versions.Spec{
		Selector: detectedK8sVersion,
	}
```

(The rest of `Prepare` from `envConfig, err := createEnvtest(...)` onward is unchanged.)

- [ ] **Step 4: Add the `--kubernetes-version` flag in `cmd/serve.go`**

Add a field to `serveOptions`:

```go
	kubernetesVersion     string
```

Register the flag inside `NewServeCommand` (next to the other `cmd.Flags().StringVar` calls):

```go
	cmd.Flags().StringVar(
		&options.kubernetesVersion, "kubernetes-version", options.kubernetesVersion,
		"override the Kubernetes version (e.g. 1.31). Auto-detected from the bundle when empty.",
	)
```

- [ ] **Step 5: Pass the override through `startK8sServer`**

In `startK8sServer`, change the `envtest.Prepare` call (currently
`envtest.Prepare(ctx, supportBundle, envtest.Arch(opts.envtestArch))`) to:

```go
	testEnv, err := envtest.Prepare(ctx, supportBundle, opts.kubernetesVersion, envtest.Arch(opts.envtestArch))
```

- [ ] **Step 6: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly. If `pkg/envtest/environment_test.go` (or other tests)
call `Prepare` with the old signature and break `go build ./...`, update those
call sites to pass `""` as the new `versionOverride` argument. (Tests are not run
this round but the tree must compile.)

- [ ] **Step 7: Commit**

```bash
git add pkg/envtest/detect_cluster_version.go pkg/envtest/setup.go cmd/serve.go
git commit -m "feat(envtest): detect Spectro server version and add --kubernetes-version override"
```

---

## Task 4: CRD and namespace paths from layout

**Files:**
- Modify: `pkg/importer/crds.go`
- Modify: `pkg/importer/import.go`

- [ ] **Step 1: Use the layout CRD filename in `pkg/importer/crds.go`**

In `loadCRDs`, replace:

```go
	crdsPath := filepath.Join(b.Layout().ClusterResources(), "custom-resource-definitions.json")
```

with:

```go
	crdsPath := filepath.Join(b.Layout().ClusterResources(), b.Layout().CRDsFile())
```

In `importCRDs`, replace the `WarnOnErrorsFilePresence` path argument:

```go
			filepath.Join(cfg.bundle.Layout().ClusterResources(), cfg.bundle.Layout().CRDsFile()),
```

- [ ] **Step 2: Use the layout namespaces filename in `pkg/importer/import.go`**

In `importNamespaces`, replace:

```go
	namespacesPath := filepath.Join(cfg.bundle.Layout().ClusterResources(), "namespaces.json")
```

with:

```go
	namespacesPath := filepath.Join(cfg.bundle.Layout().ClusterResources(), cfg.bundle.Layout().NamespacesFile())
```

- [ ] **Step 3: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add pkg/importer/crds.go pkg/importer/import.go
git commit -m "feat(importer): source CRD and namespace filenames from layout"
```

---

## Task 5: Layout-driven skip lists + conditional configmap/secret import

**Files:**
- Modify: `pkg/importer/import.go`

- [ ] **Step 1: Make the importer list conditional on layout in `ImportBundle`**

Replace the static `importers` slice (currently includes `importCMs, importSecrets`) with:

```go
	importers := []importerFn{
		importCRDs,
		importNamespaces,
		importClusterResources,
	}
	// Spectro bundles store configmaps/secrets as normal k8s List objects under
	// cluster-resources, so the generic walk imports them. Native troubleshoot.sh
	// bundles use a special struct in dedicated dirs, handled here.
	if !b.Layout().Spectro() {
		importers = append(importers, importCMs, importSecrets)
	}
```

- [ ] **Step 2: Replace the hardcoded skip-lists in `importClusterResources`**

Replace the `skipResources := []string{...}` and `skipDirs := []string{...}`
literals (currently lines ~118-133) with:

```go
	skipResources := cfg.bundle.Layout().SkipResources()
	skipDirs := cfg.bundle.Layout().SkipDirs()
```

- [ ] **Step 3: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add pkg/importer/import.go
git commit -m "feat(importer): drive skip-lists and configmap/secret handling from layout"
```

---

## Task 6: Resolve kube-system pods file by extension

**Files:**
- Modify: `pkg/bundle/service_ip_range.go`

- [ ] **Step 1: Try both `.yaml` and `.json` in `findKubeApiserverPod`**

Replace the start of `findKubeApiserverPod` (the `path := ...` + `LoadResourcesFromFile` lines):

```go
	path := filepath.Join(b.Layout().ClusterResources(), "pods", "kube-system.json")
	list, err := LoadResourcesFromFile(b, path)
	if err != nil {
		return nil, fmt.Errorf("failed to load pods from file %q: %w", path, err)
	}
```

with a small extension-probe:

```go
	var list *unstructured.UnstructuredList
	var lastErr error
	for _, name := range []string{"kube-system.yaml", "kube-system.json"} {
		path := filepath.Join(b.Layout().ClusterResources(), "pods", name)
		if exists, _ := afero.Exists(b, path); !exists {
			continue
		}
		l, err := LoadResourcesFromFile(b, path)
		if err != nil {
			lastErr = fmt.Errorf("failed to load pods from file %q: %w", path, err)
			continue
		}
		list = l
		break
	}
	if list == nil {
		// No kube-system pods file (e.g. managed providers); not fatal.
		return nil, lastErr
	}
```

- [ ] **Step 2: Ensure imports**

Confirm `pkg/bundle/service_ip_range.go` imports `"github.com/spf13/afero"` and
`"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"`. Add whichever is missing.

- [ ] **Step 3: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add pkg/bundle/service_ip_range.go
git commit -m "feat(bundle): resolve kube-system pods file as yaml or json"
```

---

## Task 7: Spectro pod-log retrieval

**Files:**
- Modify: `pkg/proxy/logs.go`

- [ ] **Step 1: Add a pod-logs directory resolver helper**

Add to `pkg/proxy/logs.go` (it already imports `afero`, `filepath`, `fmt`). Add
`"sort"`, `"strconv"`, and `"strings"` to the import block.

```go
// findVarLogPodFile locates a log file inside a /var/log/pods style layout:
//   <podLogsRoot>/<namespace>_<pod>_<uid>/<container>/<restartCount>.log
// It returns the highest restartCount file (or the second-highest when previous
// is true). Returns "" when nothing matches.
func findVarLogPodFile(b afero.Fs, podLogsRoot, namespace, pod, container string, previous bool) string {
	entries, err := afero.ReadDir(b, podLogsRoot)
	if err != nil {
		return ""
	}
	prefix := fmt.Sprintf("%s_%s_", namespace, pod)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		containerDir := filepath.Join(podLogsRoot, e.Name(), container)
		logFiles, err := afero.ReadDir(b, containerDir)
		if err != nil {
			continue
		}
		counts := []int{}
		for _, lf := range logFiles {
			if !strings.HasSuffix(lf.Name(), ".log") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSuffix(lf.Name(), ".log"))
			if err != nil {
				continue
			}
			counts = append(counts, n)
		}
		if len(counts) == 0 {
			continue
		}
		sort.Sort(sort.Reverse(sort.IntSlice(counts)))
		idx := 0
		if previous {
			idx = 1
		}
		if idx >= len(counts) {
			return ""
		}
		return filepath.Join(containerDir, fmt.Sprintf("%d.log", counts[idx]))
	}
	return ""
}
```

- [ ] **Step 2: Extend candidate-path resolution in `LogsHandler`**

Replace the candidate-path block (currently lines ~28-38, building `candidatePaths`
and the first loop) with version below. It keeps the native paths and adds the
Spectro current/previous paths.

```go
		namespace := vars["namespace"]
		pod := vars["pod"]
		container := r.URL.Query().Get("container")
		previous := r.URL.Query().Get("previous") == "true"

		filename := fmt.Sprintf("%s-%s.log", pod, container)
		candidatePaths := []string{}

		if previous {
			// Spectro previous logs.
			if p := findVarLogPodFile(b, b.Layout().PodLogs(), namespace, pod, container, true); p != "" {
				candidatePaths = append(candidatePaths, p)
			}
			candidatePaths = append(candidatePaths,
				filepath.Join(b.Layout().PreviousPodLogs(), namespace, pod, "previous.log"),
			)
		} else {
			// Native troubleshoot.sh paths.
			candidatePaths = append(candidatePaths,
				filepath.Join(b.Layout().PodLogs(), namespace, filename),
				filepath.Join(b.Layout().ClusterResources(), "pods/logs", namespace, pod, container+".log"),
			)
			// Spectro infra: kubectl cluster-info dump.
			candidatePaths = append(candidatePaths,
				filepath.Join(b.Layout().ClusterInfo(), "dump", namespace, pod, "logs.txt"),
			)
			// Spectro edge: /var/log/pods copy.
			if p := findVarLogPodFile(b, b.Layout().PodLogs(), namespace, pod, container, false); p != "" {
				candidatePaths = append(candidatePaths, p)
			}
		}

		for _, candidatePath := range candidatePaths {
			if exists, _ := afero.Exists(b, candidatePath); exists {
				podLogsPath = candidatePath
				break
			}
		}
```

- [ ] **Step 3: Build**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add pkg/proxy/logs.go
git commit -m "feat(proxy): serve Spectro pod logs (dump, pod-logs, previous-pod-logs)"
```

---

## Task 8: End-to-end verification + README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Full build + vet**

Run: `cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live && go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 2: Serve the infra bundle**

```bash
cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live
go run . serve ~/Downloads/it-vault-dev-2026-06-09_17_59_26.tar.gz
```
Expected: "Starting k8s server" succeeds (envtest downloads the detected version;
if that version is unavailable, re-run with `--kubernetes-version 1.34`), bundle
imports with few/no errors, and it prints `KUBECONFIG=...` + the proxy address.
Leave it running for the next step (or background it).

- [ ] **Step 3: Query the infra API server**

In another shell (use the printed kubeconfig path):

```bash
export KUBECONFIG=/Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live/support-bundle-kubeconfig
kubectl get ns
kubectl get pods -A | head
kubectl get deploy -A | head
# pick a real pod from the output:
kubectl -n palette-system describe pod <pod>
kubectl -n palette-system logs <pod>
```
Expected: namespaces, pods, deployments list from bundle data; `describe` shows
details; `logs` returns content from `cluster-info/dump/.../logs.txt`.
Stop the server (Ctrl-C / kill) when done.

- [ ] **Step 4: Serve + query the edge bundle**

```bash
cd /Users/chella.vignesh/Repo/troubleshoot-live-vignesh/troubleshoot-live
go run . serve ~/Downloads/edge-609e35423bb8e6209a04d5a6990a7b49-2026-05-18_14_13_41.tar.gz
```
Then in another shell, repeat the `kubectl get`/`logs` checks. For edge, also
verify a previous-log read works on a pod that has restarts:
```bash
kubectl -n <ns> logs <pod> --previous
```
Expected: current logs come from `k8s/pod-logs/<ns>_<pod>_<uid>/<container>/<N>.log`;
`--previous` resolves to `N-1.log` or `previous-pod-logs/.../previous.log`.

- [ ] **Step 5: Verify host-only bundle gives a clean error**

```bash
go run . serve ~/Downloads/edge-393832503834584d51323530304b4d4a-2026-04-23_08_34_30.tar.gz
```
Expected: fails fast with "bundle contains no Kubernetes resources; nothing to serve"
(no panic/stack trace).

- [ ] **Step 6: Document in `README.md`**

Add a section after "Usage" describing Spectro support:

```markdown
## Spectro Cloud support bundles

`troubleshoot-live` auto-detects Spectro Cloud sustaining-team support bundles
(produced by `support-tools/support-bundle-infra.sh` and `support-bundle-edge.sh`)
by the presence of a `k8s/cluster-resources/` directory and adapts automatically —
no configuration required. Native `troubleshoot.sh` bundles continue to work.

If the bundle's Kubernetes version cannot be detected, or the matching envtest
binaries are unavailable, set it explicitly:

```bash
troubleshoot-live serve support-bundle.tar.gz --kubernetes-version 1.31
```

Edge bundles collected from a host without Kubernetes (no `k8s/` directory) have
no API resources to serve and will report:
`bundle contains no Kubernetes resources; nothing to serve`.
```

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document Spectro bundle auto-detection and --kubernetes-version"
```

---

## Self-Review notes (spec coverage)

- Spec §1 layout detection → Task 1. §2 List parsing → Task 2. §3 version detection
  + override → Task 3. §4 CRDs/namespaces → Task 4. §5 walk skip-lists + CM/secret
  → Task 5. §6 pod logs + service_ip_range → Tasks 6, 7. Host-only error → Task 1
  (impl) + Task 8 (verify). envtest version risk → `--kubernetes-version` (Task 3)
  + README (Task 8).
- Method names are consistent across tasks: `Spectro()`, `ClusterVersionFile()`,
  `CRDsFile()`, `NamespacesFile()`, `SkipResources()`, `SkipDirs()`,
  `PreviousPodLogs()`, `ParseVersionSelector()`, `findVarLogPodFile()`.
- `Prepare` signature change (added `versionOverride string`) — the only known
  caller is `cmd/serve.go` (Task 3 Step 5); Step 6 covers fixing any test callers
  so the tree compiles.
```
