//go:build influx

package stage

import (
	"context"
	"encoding/json"
	"os"
	"pbench/log"
	"sync/atomic"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type InfluxRunRecorder struct {
	influxClient influxdb2.Client
	influxWriter influxapi.WriteAPIBlocking
	failed       atomic.Int64
	mismatch     atomic.Int64
}

func NewInfluxRunRecorder(cfgPath string) RunRecorder {
	if cfgPath == "" {
		return nil
	}
	if bytes, err := os.ReadFile(cfgPath); err != nil {
		log.Error().Err(err).Msg("failed to read InfluxDB connection config")
		return nil
	} else {
		influxCfg := &struct {
			Url    string `json:"url"`
			Org    string `json:"org"`
			Bucket string `json:"bucket"`
			Token  string `json:"token"`
		}{}
		if err = json.Unmarshal(bytes, influxCfg); err != nil {
			log.Error().Err(err).Msg("failed to initialize InfluxDB connection for the run recorder")
			return nil
		}
		influxClient := influxdb2.NewClient(influxCfg.Url, influxCfg.Token)
		r := &InfluxRunRecorder{
			influxClient: influxClient,
			influxWriter: influxClient.WriteAPIBlocking(influxCfg.Org, influxCfg.Bucket),
		}
		log.Info().Str("url", influxCfg.Url).Str("org", influxCfg.Org).Str("bucket", influxCfg.Bucket).
			Msg("InfluxDB connection initialized, benchmark result summary will be sent to this database.")
		return r
	}
}

func (i *InfluxRunRecorder) Start(_ context.Context, _ *Stage) error {
	return nil
}

func (i *InfluxRunRecorder) RecordQuery(ctx context.Context, s *Stage, result *QueryResult) {
	tags := map[string]string{
		"run_name": s.States.RunName,
		"stage_id": result.StageId,
		"query_id": result.QueryId,
	}
	fields := map[string]interface{}{
		"query_index":        result.Query.Index,
		"cold_run":           result.Query.ColdRun,
		"sequence_no":        result.Query.SequenceNo,
		"info_url":           result.InfoUrl,
		"succeeded":          result.QueryError == nil,
		"row_count":          result.RowCount,
		"expected_row_count": result.Query.ExpectedRowCount,
		"start_time":         result.StartTime.UnixNano(),
	}
	// Duration and EndTime are nil when ConcludeExecution was not called (e.g., query error).
	if result.Duration != nil {
		fields["duration_ms"] = result.Duration.Milliseconds()
	}
	if result.Query.ExpectedRowCount < 0 {
		delete(fields, "expected_row_count")
	} else if result.Query.ExpectedRowCount != result.RowCount {
		i.mismatch.Add(1)
	}
	if result.QueryError != nil {
		i.failed.Add(1)
	}
	if result.Query.File != nil {
		fields["query_file"] = *result.Query.File
	} else {
		fields["query_file"] = "inline"
	}
	var endTime time.Time
	if result.EndTime != nil {
		endTime = *result.EndTime
	}
	point := write.NewPoint("queries", tags, fields, endTime)
	if err := i.influxWriter.WritePoint(ctx, point); err != nil {
		log.Error().EmbedObject(result).Err(err).Msg("failed to send query summary to influxdb")
	}
}

func (i *InfluxRunRecorder) RecordRun(ctx context.Context, s *Stage, results []*QueryResult) {
	tags := map[string]string{
		"run_name": s.States.RunName,
	}
	fields := map[string]interface{}{
		"start_time":  s.States.RunStartTime.UnixNano(),
		"queries_ran": len(results),
		"failed":      i.failed.Load(),
		"mismatch":    i.mismatch.Load(),
		"duration_ms": s.States.RunFinishTime.Sub(s.States.RunStartTime).Milliseconds(),
		"comment":     s.States.Comment,
	}
	if s.States.RandSeedUsed.Load() {
		fields["rand_seed"] = s.States.RandSeed
	}
	point := write.NewPoint("runs", tags, fields, s.States.RunFinishTime)
	if err := i.influxWriter.WritePoint(ctx, point); err != nil {
		log.Error().Str("run_name", s.States.RunName).Err(err).Msg("failed to send run summary to influxdb")
	}
}

// RecordMetrics uploads query metrics to InfluxDB in Prometheus format
// metrics structure: map[host]map[category]map[metric_name]value
// Example: metrics["172_20_0_3_10010"]["presto_cpp"]["num_http_request"] = 0.0
// Field names will be: category_metric_name (e.g., "velox_memory_cache_hit_bytes")
func (i *InfluxRunRecorder) RecordMetrics(ctx context.Context, queryId string, metrics map[string]map[string]map[string]float64, timestamp *time.Time) {
	if metrics == nil || len(metrics) == 0 {
		return
	}

	// Use current time if timestamp not provided
	var ts time.Time
	if timestamp != nil {
		ts = *timestamp
	} else {
		ts = time.Now()
	}

	// Collect all points into a slice for batch writing
	var points []*write.Point
	totalMetrics := 0

	// Iterate through all hosts, categories, and metrics
	for host, categories := range metrics {
		for category, metricMap := range categories {
			for metricName, value := range metricMap {
				// Create tags similar to Telegraf's Prometheus input
				tags := map[string]string{
					"query_id":  queryId,
					"host":      host,
					"component": category, // "presto_cpp" or "velox"
				}

				// Field name format: category_metric_name (e.g., "velox_memory_cache_hit_bytes")
				fieldName := category + "_" + metricName
				fields := map[string]interface{}{
					fieldName: value,
				}

				// Use "prometheus" as measurement name to match Telegraf format
				point := write.NewPoint("prometheus", tags, fields, ts)
				points = append(points, point)
				totalMetrics++
			}
		}
	}

	// Write all points in a single batch request
	if len(points) > 0 {
		if err := i.influxWriter.WritePoint(ctx, points...); err != nil {
			log.Error().
				Str("query_id", queryId).
				Int("total_metrics", totalMetrics).
				Int("total_hosts", len(metrics)).
				Err(err).
				Msg("failed to send metrics to influxdb")
			return
		}
	}

	log.Info().
		Str("query_id", queryId).
		Int("total_metrics", totalMetrics).
		Int("total_hosts", len(metrics)).
		Msg("uploaded metrics to influxdb")
}
