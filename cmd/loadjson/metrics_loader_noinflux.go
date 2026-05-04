//go:build !influx

package loadjson

import (
	"context"
)

// MetricsInfluxWriter is a no-op when influx build tag is not present
type MetricsInfluxWriter struct{}

// NewMetricsInfluxWriter returns nil when influx is not available
func NewMetricsInfluxWriter(cfgPath string) *MetricsInfluxWriter {
	return nil
}

// WriteMetricsFromJSON is a no-op when influx is not available
func (m *MetricsInfluxWriter) WriteMetricsFromJSON(ctx context.Context, jsonData []byte, queryID string) error {
	return nil
}

// Made with Bob
