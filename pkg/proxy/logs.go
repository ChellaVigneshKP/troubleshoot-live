package proxy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/afero"

	"github.com/mhrabovcin/troubleshoot-live/pkg/bundle"
)

// LogsHandler serves logs for k8s `logs` subresource from the provided bundle.
func LogsHandler(b bundle.Bundle, l *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)

		podLogsPath := ""

		namespace := vars["namespace"]
		pod := vars["pod"]
		container := r.URL.Query().Get("container")
		previous := r.URL.Query().Get("previous") == "true"

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
			filename := fmt.Sprintf("%s-%s.log", pod, container)
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

		if podLogsPath == "" {
			http.Error(w, "pod logs not found in the bundle", http.StatusInternalServerError)
			return
		}

		data, err := afero.ReadFile(b, podLogsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		l := l.With("url", r.URL, "logs source", podLogsPath)

		// By default the `k9s` requests logs prefixed with timestamp and in the logs pane
		// only displays a portion without the timestamp, by cutting prefix separated by first
		// space byte(' '). The troubleshoot.sh requests logs without timestamps, which causes
		// issues in the logs pane and for some pods the logs are cut from beginnging.
		// This will backfill zeroed timestamp for each line.
		if r.URL.Query().Get("timestamps") == "true" {
			lines := bytes.Split(data, []byte("\n"))
			timestampPrefixRegexp := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{6})?Z `)
			if !timestampPrefixRegexp.Match(lines[0]) {
				l.Debug("adding timestamp prefix to logs")
				zeroTime := []byte(time.UnixMicro(0).Format(time.RFC3339Nano))
				// Add prefix to each line.
				for i := range lines {
					lines[i] = bytes.Join([][]byte{zeroTime, lines[i]}, []byte{' '})
				}
				data = bytes.Join(lines, []byte("\n"))
			}
		}

		l.Debug("serving logs")
		if _, err := w.Write(data); err != nil {
			slog.Error("failed to write response data", "err", err)
		}
	}
}

// findVarLogPodFile locates a log file inside a /var/log/pods style layout:
//
//	<podLogsRoot>/<namespace>_<pod>_<uid>/<container>/<restartCount>.log
//
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
