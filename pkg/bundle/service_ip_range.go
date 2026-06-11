package bundle

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const apiServerContainerName = "kube-apiserver"

// DetectServiceSubnetRange attempts to determine service ip range value provided
// to k8s api server, so that local version can be launched with same argument.
// So far the function tries to parse value from `kube-apiserver` pod.
// Other potential locations for parsing this value:
// - CAPI cluster resource
// - KIND kubeadm config.
func DetectServiceSubnetRange(b Bundle) (string, error) {
	apiServerPod, err := findKubeApiserverPod(b)
	if err != nil {
		return "", err
	}

	// Some bundles collected from managed providers, like gke, eks would not have
	// the kube-apiserver pod.
	if apiServerPod == nil {
		return "", nil
	}

	return parseIPRangeArg(apiServerPod)
}

// DetectServiceNodePortRange attempts to determine service node port range value provided
// to k8s api server, so that local version can be launched with same argument.
func DetectServiceNodePortRange(b Bundle) (string, error) {
	apiServerPod, err := findKubeApiserverPod(b)
	if err != nil {
		return "", err
	}

	// Some bundles collected from managed providers, like gke, eks would not have
	// the kube-apiserver pod.
	if apiServerPod == nil {
		return "", nil
	}

	return parseNodePortRangeArg(apiServerPod)
}

func findKubeApiserverPod(b Bundle) (*corev1.Pod, error) {
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

	for i := range list.Items {
		pod := &corev1.Pod{}
		if err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(list.Items[i].UnstructuredContent(), &pod); err != nil {
			return nil, err
		}

		if isKubeApiserverPod(pod) {
			return pod, nil
		}
	}

	return nil, nil
}

func parseNodePortRangeArg(pod *corev1.Pod) (string, error) {
	for _, c := range pod.Spec.Containers {
		if c.Name != apiServerContainerName {
			continue
		}

		for _, arg := range c.Command {
			if strings.HasPrefix(arg, "--service-node-port-range=") {
				return strings.TrimPrefix(arg, "--service-node-port-range="), nil
			}
		}
	}

	return "", nil
}

func parseIPRangeArg(pod *corev1.Pod) (string, error) {
	for _, c := range pod.Spec.Containers {
		if c.Name != apiServerContainerName {
			continue
		}

		for _, arg := range c.Command {
			if strings.HasPrefix(arg, "--service-cluster-ip-range=") {
				return strings.TrimPrefix(arg, "--service-cluster-ip-range="), nil
			}
		}
	}

	return "", nil
}

func isKubeApiserverPod(pod *corev1.Pod) bool {
	if !strings.HasPrefix(pod.GetName(), "kube-apiserver-") {
		return false
	}

	labels := pod.GetLabels()
	return labels["component"] == "kube-apiserver"
}
