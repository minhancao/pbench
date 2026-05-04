package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pbench/log"
)

// MetricsSnapshot represents metrics collected at a specific point in time
type MetricsSnapshot struct {
	Timestamp string                   `json:"timestamp"`
	Workers   map[string]WorkerMetrics `json:"workers"`
}

// WorkerMetrics contains metrics from a single worker node
type WorkerMetrics struct {
	PrestoCpp map[string]interface{} `json:"presto_cpp"`
	Velox     map[string]interface{} `json:"velox"`
}

// MetricsCollection contains metrics snapshots at different times
type MetricsCollection struct {
	StartTime string                     `json:"start_time"`
	EndTime   string                     `json:"end_time"`
	Snapshots map[string]MetricsSnapshot `json:"snapshots"`
}

// CollectWorkerMetrics fetches metrics from all worker nodes involved in a query
func (s *Stage) CollectWorkerMetrics(ctx context.Context, queryInfo map[string]interface{}) (map[string]WorkerMetrics, error) {
	workerURIs := extractWorkerURIs(queryInfo)
	if len(workerURIs) == 0 {
		return nil, fmt.Errorf("no worker URIs found in query info")
	}

	metrics := make(map[string]WorkerMetrics)
	for _, workerURI := range workerURIs {
		workerID := workerIDFromURI(workerURI)
		workerMetrics, err := fetchWorkerMetrics(ctx, workerURI)
		if err != nil {
			log.Warn().Err(err).Str("worker_uri", workerURI).Msg("failed to fetch worker metrics")
			continue
		}
		metrics[workerID] = workerMetrics
	}

	return metrics, nil
}

// extractWorkerURIs extracts unique worker URIs from query info by traversing the stage tree
func extractWorkerURIs(queryInfo map[string]interface{}) []string {
	workerURISet := make(map[string]bool)

	outputStage, ok := queryInfo["outputStage"].(map[string]interface{})
	if !ok {
		return nil
	}

	stack := []map[string]interface{}{outputStage}

	for len(stack) > 0 {
		stageInfo := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if stageInfo == nil {
			continue
		}

		// Get latest attempt execution info
		latestAttempt, ok := stageInfo["latestAttemptExecutionInfo"].(map[string]interface{})
		if !ok {
			continue
		}

		// Extract tasks
		tasks, ok := latestAttempt["tasks"].([]interface{})
		if ok {
			for _, taskInterface := range tasks {
				task, ok := taskInterface.(map[string]interface{})
				if !ok {
					continue
				}

				taskStatus, ok := task["taskStatus"].(map[string]interface{})
				if !ok {
					continue
				}

				taskSelf, ok := taskStatus["self"].(string)
				if !ok {
					continue
				}

				// Parse URL to extract worker URI
				parsed, err := url.Parse(taskSelf)
				if err != nil {
					continue
				}

				workerURI := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
				workerURISet[workerURI] = true
			}
		}

		// Add substages to stack
		subStages, ok := stageInfo["subStages"].([]interface{})
		if ok {
			for _, subStageInterface := range subStages {
				if subStage, ok := subStageInterface.(map[string]interface{}); ok {
					stack = append(stack, subStage)
				}
			}
		}
	}

	// Convert set to slice
	workerURIs := make([]string, 0, len(workerURISet))
	for uri := range workerURISet {
		workerURIs = append(workerURIs, uri)
	}

	return workerURIs
}

// fetchWorkerMetrics fetches and parses metrics from a single worker node
func fetchWorkerMetrics(ctx context.Context, workerURI string) (WorkerMetrics, error) {
	metricsURL := fmt.Sprintf("%s/v1/info/metrics", workerURI)

	req, err := http.NewRequestWithContext(ctx, "GET", metricsURL, nil)
	if err != nil {
		return WorkerMetrics{}, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return WorkerMetrics{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return WorkerMetrics{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WorkerMetrics{}, err
	}

	// Parse Prometheus format metrics
	metricsList := parsePrometheusMetrics(string(body))

	// Convert to nested object structure
	return metricsToNestedObject(metricsList), nil
}

// parsePrometheusMetrics parses Prometheus text format into a list of metric name/value pairs
func parsePrometheusMetrics(text string) []struct {
	name  string
	value float64
} {
	var metrics []struct {
		name  string
		value float64
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on last space to separate metric name from value
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}

		// Get metric name (strip labels if present)
		metricName := parts[0]
		if idx := strings.Index(metricName, "{"); idx != -1 {
			metricName = metricName[:idx]
		}

		// Parse value
		valueStr := parts[len(parts)-1]
		var value float64
		if _, err := fmt.Sscanf(valueStr, "%f", &value); err != nil {
			continue
		}

		metrics = append(metrics, struct {
			name  string
			value float64
		}{metricName, value})
	}

	return metrics
}

// metricsToNestedObject converts a list of metrics to nested object structure
func metricsToNestedObject(metrics []struct {
	name  string
	value float64
}) WorkerMetrics {
	prestoCpp := make(map[string]interface{})
	velox := make(map[string]interface{})

	for _, metric := range metrics {
		var value interface{} = metric.value

		// Sanitize NaN and Inf values
		if math.IsNaN(metric.value) || math.IsInf(metric.value, 0) {
			value = nil
		}

		if strings.HasPrefix(metric.name, "presto_cpp_") {
			key := strings.TrimPrefix(metric.name, "presto_cpp_")
			prestoCpp[key] = value
		} else if strings.HasPrefix(metric.name, "velox_") {
			key := strings.TrimPrefix(metric.name, "velox_")
			velox[key] = value
		}
	}

	return WorkerMetrics{
		PrestoCpp: prestoCpp,
		Velox:     velox,
	}
}

// workerIDFromURI extracts a filesystem-safe worker ID from a URI
func workerIDFromURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return strings.ReplaceAll(strings.ReplaceAll(uri, ":", "_"), ".", "_")
	}
	return strings.ReplaceAll(strings.ReplaceAll(parsed.Host, ":", "_"), ".", "_")
}

// FetchQueryInfoAsMap fetches query info and returns it as a map for metrics collection
func (s *Stage) FetchQueryInfoAsMap(ctx context.Context, queryID string) (map[string]interface{}, error) {
	// Create a temporary buffer to capture the JSON
	var buf strings.Builder

	// Use the existing GetQueryInfo method but capture to a string
	_, err := s.Client.GetQueryInfo(ctx, queryID, &buf)
	if err != nil {
		return nil, err
	}

	// Parse the JSON into a map
	var queryInfo map[string]interface{}
	if err := json.Unmarshal([]byte(buf.String()), &queryInfo); err != nil {
		return nil, err
	}

	return queryInfo, nil
}

// Made with Bob
