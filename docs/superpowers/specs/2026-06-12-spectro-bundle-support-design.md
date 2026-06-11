# Spectro support-bundle support for troubleshoot-live

Date: 2026-06-12
Status: Approved design (pre-implementation)

## Goal

`troubleshoot-live serve <spectro-bundle.tar.gz>` works end-to-end with **zero
config** for Spectro Cloud sustaining-team support bundles (both the *infra* and
*edge* collectors), while native `troubleshoot.sh` bundles continue to work
unchanged.

"Works end-to-end" means: the local API server starts at the bundle's K8s
version, all resources (including CRDs and custom resources) import, and
`kubectl get` / `describe` / `logs` work against the served API.

This is functional parity with the `pavansokkenagaraj` fork **plus** the fixes
that fork does not address (kubectl `List` YAML parsing, server-version
detection, CRD/namespace YAML filenames, configmap/secret handling), achieved
via auto-detection rather than a hand-maintained `config.yaml`.

## Background — why upstream does not work as-is

Your fork is upstream `mhrabovcin/troubleshoot-live` at HEAD (v0.2.0 + #182/#183).
Upstream targets **native troubleshoot.sh bundles** (JSON files, no `k8s/`
prefix, troubleshoot-specific configmap/secret structs). Spectro bundles are a
`kubectl`-based YAML dump produced by `support-tools/support-bundle-{infra,edge}.sh`.

Confirmed differences (against real bundles `it-vault-dev-…` and
`edge-609e…`):

| Concern | Upstream expects | Spectro bundle |
|---|---|---|
| Root prefix | `cluster-info/`, `cluster-resources/` | `k8s/cluster-info/`, `k8s/cluster-resources/` |
| Version file | `cluster-info/cluster_version.json` (`{info,string}`) | `k8s/cluster-info/cluster-version.yaml` (`kubectl version -o yaml`; use `serverVersion.gitVersion`) |
| CRDs | `cluster-resources/custom-resource-definitions.json` | `k8s/cluster-resources/crds.yaml` |
| Namespaces | `cluster-resources/namespaces.json` | `k8s/cluster-resources/namespaces.yaml` |
| Resource files | per-resource JSON | `kubectl get -o yaml` → **`kind: List` YAML map** |
| ConfigMaps/Secrets | troubleshoot `{name,namespace,data}` JSON | normal k8s objects in `…/configmaps/<ns>.yaml`, `…/secrets/<ns>.yaml` |
| Custom resources | n/a | `k8s/cluster-resources/custom-resources/<CRD>[/<ns>].yaml` |
| Current pod logs | `pod-logs/…` | infra: `k8s/cluster-info/dump/<ns>/<pod>/logs.txt`; edge: also `k8s/pod-logs/<ns>_<pod>_<uid>/<container>/<N>.log` |
| Previous pod logs | restart-count `.log` | `k8s/previous-pod-logs/<ns>/<pod>/previous.log` |

The single biggest blocker: `LoadResourcesFromFile`'s YAML branch only accepts a
bare YAML array, but every Spectro resource file is a `kind: List` map, so
nearly all imports would fail.

## Non-goals (YAGNI)

- Importing edge **host-level** artifacts (etcd, journald, helm, crictl, var/log,
  …) into the API server. Out of scope for K8s API serving.
- A `config.yaml` layout-override mechanism. Superseded by auto-detection; can be
  added later if a non-conforming bundle appears.
- Unit tests. Out of scope for this round (per request). Verification is
  end-to-end against the two real sample bundles.

## Design

### 1. Layout detection (`pkg/bundle`)
- Add `spectroLayout` implementing `Layout`, returning the `k8s/`-prefixed paths.
- Extend the `Layout` interface with `PreviousPodLogs()`, `SkipResources()`,
  `SkipDirs()`, and the special filenames (`ClusterVersionFile()`,
  `CRDsFile()`, `NamespacesFile()`) plus a `Format()`/`IsSpectro()` discriminator
  so dependent code (version parse, configmap/secret handling) can branch.
- In `bundle.New(...)`, after the root dir is resolved, probe for
  `k8s/cluster-resources/` → choose `spectroLayout`, else `defaultLayout`.
  Store the chosen layout on the `bundle` struct (Pavan's pattern, auto-selected).
- If neither a Spectro `k8s/cluster-resources/` nor a native `cluster-resources/`
  exists → return a clear error: *"bundle contains no Kubernetes resources;
  nothing to serve"* (covers host-only edge bundles).

### 2. Format-agnostic resource loading (`pkg/bundle/resources.go`) — critical
- Fix the YAML branch of `LoadResourcesFromFile` to handle, in order:
  a `kind: List` map (`.items`), a bare YAML array, and a single object.
- This is a general robustness improvement (upstreamable).

### 3. Version detection (`pkg/envtest/detect_cluster_version.go`)
- Branch on layout: Spectro → read `cluster-version.yaml`, parse the
  `kubectl version -o yaml` shape, use `serverVersion.gitVersion`
  (falling back to `serverVersion.major/minor`). Native → existing JSON path.
- If the version file is missing/unparseable, honor a new `--kubernetes-version`
  flag; otherwise fall back to the nearest available envtest version.

### 4. CRDs & namespaces (`pkg/importer/crds.go`, `import.go`)
- Source the CRD path and namespaces path from the layout (`crds.yaml` /
  `namespaces.yaml` vs the native `*.json`). Existing v1beta1→v1 conversion and
  the List-parse fix handle `kubectl get crd -o yaml`.

### 5. Cluster-resources walk, skip lists, ordering (`pkg/importer/import.go`)
- Move skip lists onto the layout. Spectro skips: `crds.yaml`, `namespaces.yaml`,
  `mutatingwebhookconfigurations.yaml`, `validatingwebhookconfigurations.yaml`,
  `apiservices.yaml`, and dirs `apiservices`, `auth-cani-list`,
  `pod-disruption-budgets`, plus non-resource files (`api-resources`,
  `cluster-info`).
- `custom-resources/` is walked after `importCRDs` (current ordering already does
  CRDs first — verify), so CR GVRs resolve.
- ConfigMaps/Secrets: for the Spectro layout, do **not** run the troubleshoot
  `LoadConfigMap`/`LoadSecret` path; the generic walk imports
  `cluster-resources/{configmaps,secrets}/<ns>.yaml` as normal k8s `List`
  objects. Guard against double-import.

### 6. Pod logs (`pkg/proxy/logs.go`, `pkg/bundle/service_ip_range.go`)
- Extend the candidate-path search (Pavan's logic + edge):
  - current: `cluster-info/dump/<ns>/<pod>/logs.txt`,
    `pod-logs/<ns>_<pod>_<uid>/<container>/<N>.log`
  - previous: `pod-logs/<ns>_<pod>_<uid>/<container>/<N-1>.log`,
    `previous-pod-logs/<ns>/<pod>/previous.log`
  - keep native upstream paths.
- `service_ip_range.go`: source the `pods/kube-system` file via the layout
  (`.yaml` vs `.json`).

## Risks
- **envtest binary availability**: the detected server version (e.g. 1.35.2) must
  be downloadable by `setup-envtest`. Mitigation: `--kubernetes-version` override
  + nearest-available fallback. Documented in README.
- **Host-only edge bundles**: handled with a clear error (see §1).

## Verification (end-to-end, no unit tests this round)
- Infra: `it-vault-dev-2026-06-09_17_59_26.tar.gz`
- Edge (with k8s): `edge-609e35423bb8e6209a04d5a6990a7b49-2026-05-18_14_13_41.tar.gz`
- For each: `serve` starts; `kubectl get ns/pods/deploy -A`, `describe`, and
  `logs` (current + `--previous`) return bundle data.
- Regression: a native troubleshoot.sh bundle still serves (layout falls back to
  default).
