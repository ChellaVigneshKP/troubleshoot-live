package envtest

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/afero"
	versions "sigs.k8s.io/controller-runtime/tools/setup-envtest/versions"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/mhrabovcin/troubleshoot-live/pkg/bundle"
)

//	{
//	  "info": {
//	    "major": "1",
//	    "minor": "25",
//	    "gitVersion": "v1.25.5",
//	    "gitCommit": "804d6167111f6858541cef440ccc53887fbbc96a",
//	    "gitTreeState": "clean",
//	    "buildDate": "2023-02-15T11:49:50Z",
//	    "goVersion": "go1.19.4",
//	    "compiler": "gc",
//	    "platform": "linux/amd64"
//	  },
//	  "string": "v1.25.5"
//	}
type clusterInfo struct {
	Info struct {
		Major      string `json:"major"`
		Minor      string `json:"minor"`
		GitVersion string `json:"gitVersion"`
	} `json:"info"`
	VersionString string `json:"string"`
}

func selectorFromSemver(sv *semver.Version) versions.Selector {
	// return versions.Concrete{
	// 	Major: int(sv.Major()),
	// 	Minor: int(sv.Minor()),
	// 	Patch: int(sv.Patch()),
	// }
	// default storage bucket does not contain all versions
	//
	return versions.PatchSelector{
		Major: int(sv.Major()),
		Minor: int(sv.Minor()),
		Patch: versions.AnyPoint,
	}
}

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

// ParseVersionSelector parses a user-provided version string (e.g. "1.31",
// "v1.31.2") into an envtest version selector.
func ParseVersionSelector(version string) (versions.Selector, error) {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid kubernetes version %q: %w", version, err)
	}
	return selectorFromSemver(sv), nil
}
