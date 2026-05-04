package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pbench/utils"
)

// saveQueryJsonWithMetrics saves query JSON with enhanced metrics format
func (s *Stage) saveQueryJsonWithMetrics(result *QueryResult, querySourceStr string) error {
	// Fetch the query info
	qCtx, qCancel := context.WithTimeout(context.Background(), time.Second*5)
	defer qCancel()

	var buf strings.Builder
	_, err := s.Client.GetQueryInfo(qCtx, result.QueryId, &buf)
	if err != nil {
		return err
	}

	// Parse query info as JSON
	var queryInfo map[string]interface{}
	if err := json.Unmarshal([]byte(buf.String()), &queryInfo); err != nil {
		return err
	}

	// Build the enhanced structure with metrics
	combined := make(map[string]interface{})
	combined["query"] = queryInfo

	// Extract tasks from query info (already embedded in the stage tree)
	tasks := extractEmbeddedTasks(queryInfo)
	if len(tasks) > 0 {
		combined["tasks"] = tasks
	}

	// Add metrics in the new snapshot format
	if result.MetricsAtStart != nil || result.MetricsAtEnd != nil {
		metricsCollection := MetricsCollection{
			Snapshots: make(map[string]MetricsSnapshot),
		}

		if !result.StartTime.IsZero() {
			metricsCollection.StartTime = result.StartTime.Format(time.RFC3339Nano)
		}
		if result.EndTime != nil {
			metricsCollection.EndTime = result.EndTime.Format(time.RFC3339Nano)
		}

		if result.MetricsAtStart != nil {
			metricsCollection.Snapshots["at_start"] = MetricsSnapshot{
				Timestamp: result.StartTime.Format(time.RFC3339Nano),
				Workers:   result.MetricsAtStart,
			}
		}

		if result.MetricsAtEnd != nil {
			endTime := result.StartTime
			if result.EndTime != nil {
				endTime = *result.EndTime
			}
			metricsCollection.Snapshots["at_end"] = MetricsSnapshot{
				Timestamp: endTime.Format(time.RFC3339Nano),
				Workers:   result.MetricsAtEnd,
			}
		}

		combined["metrics"] = metricsCollection
	}

	// Write the combined JSON file
	queryJsonFile, err := os.OpenFile(
		filepath.Join(s.States.OutputPath, querySourceStr)+".presto_metrics.json",
		utils.OpenNewFileFlags, 0644)
	if err != nil {
		return err
	}
	defer queryJsonFile.Close()

	encoder := json.NewEncoder(queryJsonFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(combined)
}

// extractEmbeddedTasks extracts task data from query info stage tree, grouped by worker
func extractEmbeddedTasks(queryInfo map[string]interface{}) map[string][]interface{} {
	tasksByWorker := make(map[string][]interface{})

	outputStage, ok := queryInfo["outputStage"].(map[string]interface{})
	if !ok {
		return tasksByWorker
	}

	stack := []map[string]interface{}{outputStage}

	for len(stack) > 0 {
		stageInfo := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if stageInfo == nil {
			continue
		}

		latestAttempt, ok := stageInfo["latestAttemptExecutionInfo"].(map[string]interface{})
		if !ok {
			continue
		}

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

				parsed, err := url.Parse(taskSelf)
				if err != nil {
					continue
				}

				workerID := workerIDFromURI(fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host))
				tasksByWorker[workerID] = append(tasksByWorker[workerID], task)
			}
		}

		subStages, ok := stageInfo["subStages"].([]interface{})
		if ok {
			for _, subStageInterface := range subStages {
				if subStage, ok := subStageInterface.(map[string]interface{}); ok {
					stack = append(stack, subStage)
				}
			}
		}
	}

	return tasksByWorker
}

// Made with Bob
