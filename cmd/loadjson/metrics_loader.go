//go:build influx

package loadjson

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"pbench/log"
	"pbench/stage"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

// MetricsInfluxWriter handles writing worker metrics to InfluxDB
type MetricsInfluxWriter struct {
	influxClient influxdb2.Client
	influxWriter influxapi.WriteAPIBlocking
}

// NewMetricsInfluxWriter creates a new metrics writer for InfluxDB
func NewMetricsInfluxWriter(cfgPath string) *MetricsInfluxWriter {
	if cfgPath == "" {
		return nil
	}

	influxCfg, client, writer := initInfluxConnection(cfgPath)
	if client == nil {
		return nil
	}

	log.Info().
		Str("url", influxCfg.Url).
		Str("org", influxCfg.Org).
		Str("bucket", influxCfg.Bucket).
		Msg("InfluxDB metrics writer initialized")

	return &MetricsInfluxWriter{
		influxClient: client,
		influxWriter: writer,
	}
}

// WriteMetricsFromJSON parses the enhanced JSON format and writes metrics to InfluxDB
func (m *MetricsInfluxWriter) WriteMetricsFromJSON(ctx context.Context, jsonData []byte, queryID string) error {
	if m == nil {
		return nil
	}

	// Parse the JSON to extract metrics
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return err
	}

	// Check if this is an enhanced JSON with metrics
	metricsData, hasMetrics := data["metrics"].(map[string]interface{})
	if !hasMetrics {
		log.Debug().Str("query_id", queryID).Msg("no metrics section found in JSON")
		return nil
	}

	// Extract metrics collection
	metricsCollection, err := parseMetricsCollection(metricsData)
	if err != nil {
		log.Warn().Err(err).Str("query_id", queryID).Msg("failed to parse metrics collection")
		return err
	}

	// Write metrics to InfluxDB
	return m.writeMetricsCollection(ctx, queryID, metricsCollection)
}

// parseMetricsCollection parses the metrics collection from JSON
func parseMetricsCollection(data map[string]interface{}) (*stage.MetricsCollection, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var collection stage.MetricsCollection
	if err := json.Unmarshal(jsonBytes, &collection); err != nil {
		return nil, err
	}

	return &collection, nil
}

// writeMetricsCollection writes all metrics snapshots to InfluxDB
func (m *MetricsInfluxWriter) writeMetricsCollection(ctx context.Context, queryID string, collection *stage.MetricsCollection) error {
	points := make([]*write.Point, 0)

	// Process each snapshot (at_start, at_end)
	for snapshotType, snapshot := range collection.Snapshots {
		snapshotPoints, err := m.createPointsForSnapshot(queryID, snapshotType, &snapshot)
		if err != nil {
			log.Warn().Err(err).Str("query_id", queryID).Str("snapshot", snapshotType).
				Msg("failed to create points for snapshot")
			continue
		}
		points = append(points, snapshotPoints...)
	}

	// Write all points in batch
	if len(points) > 0 {
		if err := m.influxWriter.WritePoint(ctx, points...); err != nil {
			log.Error().Err(err).Str("query_id", queryID).Int("points", len(points)).
				Msg("failed to write metrics to InfluxDB")
			return err
		}
		log.Info().Str("query_id", queryID).Int("points", len(points)).
			Msg("successfully wrote metrics to InfluxDB")
	}

	return nil
}

// createPointsForSnapshot creates InfluxDB points for a single snapshot
func (m *MetricsInfluxWriter) createPointsForSnapshot(queryID, snapshotType string, snapshot *stage.MetricsSnapshot) ([]*write.Point, error) {
	points := make([]*write.Point, 0)

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339Nano, snapshot.Timestamp)
	if err != nil {
		return nil, err
	}

	// Process each worker's metrics
	for workerID, workerMetrics := range snapshot.Workers {
		// Process presto_cpp metrics
		for metricName, metricValue := range workerMetrics.PrestoCpp {
			point := m.createMetricPoint(queryID, workerID, snapshotType, "presto_cpp", metricName, metricValue, timestamp)
			if point != nil {
				points = append(points, point)
			}
		}

		// Process velox metrics
		for metricName, metricValue := range workerMetrics.Velox {
			point := m.createMetricPoint(queryID, workerID, snapshotType, "velox", metricName, metricValue, timestamp)
			if point != nil {
				points = append(points, point)
			}
		}
	}

	return points, nil
}

// createMetricPoint creates a single InfluxDB point for a metric
func (m *MetricsInfluxWriter) createMetricPoint(queryID, workerID, snapshotType, component, metricName string, metricValue interface{}, timestamp time.Time) *write.Point {
	// Convert worker ID back to host format (e.g., 172_20_0_3_10010 -> 172.20.0.3:10010)
	host := workerIDToHost(workerID)

	tags := map[string]string{
		"query_id":      queryID,
		"host":          host,
		"component":     component,
		"snapshot_type": snapshotType,
	}

	// Handle different value types
	var value float64
	switch v := metricValue.(type) {
	case float64:
		value = v
	case int:
		value = float64(v)
	case int64:
		value = float64(v)
	case nil:
		// Skip nil values
		return nil
	default:
		log.Warn().
			Str("metric", metricName).
			Str("type", "unknown").
			Msg("skipping metric with unsupported value type")
		return nil
	}

	fields := map[string]interface{}{
		metricName: value,
	}

	// Use "prometheus" as measurement name to match telegraf format
	return write.NewPoint("prometheus", tags, fields, timestamp)
}

// workerIDToHost converts worker ID format to host:port format
// e.g., "172_20_0_3_10010" -> "172.20.0.3:10010"
func workerIDToHost(workerID string) string {
	// Simple conversion: replace underscores with dots/colons
	// Format: IP_IP_IP_IP_PORT -> IP.IP.IP.IP:PORT
	result := ""
	parts := 0
	for _, ch := range workerID {
		if ch == '_' {
			parts++
			if parts < 4 {
				result += "."
			} else {
				result += ":"
			}
		} else {
			result += string(ch)
		}
	}
	return result
}

// initInfluxConnection initializes InfluxDB connection (shared helper)
func initInfluxConnection(cfgPath string) (*influxConfig, influxdb2.Client, influxapi.WriteAPIBlocking) {
	bytes, err := readFile(cfgPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to read InfluxDB connection config")
		return nil, nil, nil
	}

	influxCfg := &influxConfig{}
	if err = json.Unmarshal(bytes, influxCfg); err != nil {
		log.Error().Err(err).Msg("failed to parse InfluxDB connection config")
		return nil, nil, nil
	}

	client := influxdb2.NewClient(influxCfg.Url, influxCfg.Token)
	writer := client.WriteAPIBlocking(influxCfg.Org, influxCfg.Bucket)

	return influxCfg, client, writer
}

type influxConfig struct {
	Url    string `json:"url"`
	Org    string `json:"org"`
	Bucket string `json:"bucket"`
	Token  string `json:"token"`
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Made with Bob
