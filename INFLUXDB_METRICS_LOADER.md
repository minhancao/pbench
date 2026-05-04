# InfluxDB Metrics Loader for pbench loadjson

## Overview

This enhancement extends the `pbench loadjson` command to automatically parse and load worker metrics from the enhanced JSON format into InfluxDB. This allows you to analyze metrics captured at query start and end times using Grafana and Flux queries.

## Features

- **Automatic Detection**: Detects enhanced JSON files with metrics sections
- **Dual Snapshot Support**: Loads both `at_start` and `at_end` metrics snapshots
- **Worker-Level Granularity**: Stores metrics per worker node
- **Component Separation**: Separates `presto_cpp` and `velox` metrics
- **Grafana Compatible**: Uses the same format as Telegraf for seamless Grafana integration

## InfluxDB Data Model

### Measurement
- **Name**: `prometheus` (matches Telegraf format)

### Tags
- `query_id`: The Presto query ID
- `host`: Worker node address (e.g., `172.20.0.3:10010`)
- `component`: Metric category (`presto_cpp` or `velox`)
- `snapshot_type`: When metrics were captured (`at_start` or `at_end`)

### Fields
- Each metric becomes a field with its name as the key and value as a float64

### Timestamp
- Uses the snapshot timestamp from the JSON

## Usage

### Basic Usage

```bash
# Load JSON files with metrics into InfluxDB
pbench loadjson --influx /path/to/influx-config.json /path/to/metrics/files/*.presto_metrics.json
```

### With Run Recording

```bash
# Load and record as a run
pbench loadjson \
  --influx /path/to/influx-config.json \
  --record-run \
  --name my_benchmark_run \
  /path/to/metrics/files/*.presto_metrics.json
```

### InfluxDB Configuration File

Create a JSON file with your InfluxDB connection details:

```json
{
  "url": "http://localhost:8086",
  "org": "my-org",
  "bucket": "presto_performance",
  "token": "your-influx-token-here"
}
```

## Example Grafana Flux Query

Query metrics for a specific query ID:

```flux
import "date"

from(bucket: "presto_performance")
  |> range(start: v.timeRangeStart, stop: date.add(d: 1s, to: v.timeRangeStop))
  |> filter(fn: (r) => r["_measurement"] == "prometheus")
  |> filter(fn: (r) => r["query_id"] =~ /.*${query}.*/)
  |> group(columns: ["_measurement", "query_id", "host", "component", "_field"])
  |> last()
  |> group()
  |> rename(columns: {_field: "metric", _value: "value"})
  |> keep(columns: ["metric", "value", "host", "component"])
  |> sort(columns: ["metric", "host", "component"])
```

### Compare Start vs End Metrics

```flux
import "date"

// Get start metrics
start = from(bucket: "presto_performance")
  |> range(start: v.timeRangeStart, stop: v.timeRangeStop)
  |> filter(fn: (r) => r["_measurement"] == "prometheus")
  |> filter(fn: (r) => r["query_id"] == "${query_id}")
  |> filter(fn: (r) => r["snapshot_type"] == "at_start")
  |> group(columns: ["host", "component", "_field"])
  |> last()
  |> set(key: "snapshot", value: "start")

// Get end metrics  
end = from(bucket: "presto_performance")
  |> range(start: v.timeRangeStart, stop: v.timeRangeStop)
  |> filter(fn: (r) => r["_measurement"] == "prometheus")
  |> filter(fn: (r) => r["query_id"] == "${query_id}")
  |> filter(fn: (r) => r["snapshot_type"] == "at_end")
  |> group(columns: ["host", "component", "_field"])
  |> last()
  |> set(key: "snapshot", value: "end")

// Union and calculate delta
union(tables: [start, end])
  |> pivot(rowKey: ["host", "component", "_field"], columnKey: ["snapshot"], valueColumn: "_value")
  |> map(fn: (r) => ({
      r with
      delta: r.end - r.start
    }))
  |> keep(columns: ["host", "component", "_field", "start", "end", "delta"])
```

## Data Flow

```
Enhanced JSON File
    ↓
pbench loadjson
    ↓
Parse metrics section
    ↓
Extract snapshots (at_start, at_end)
    ↓
For each worker:
  For each metric:
    Create InfluxDB point
    ↓
Write to InfluxDB
    ↓
Query with Grafana
```

## Example Data Points

For a query with ID `20260428_183347_00004_p2pbr`:

### At Start
```
prometheus,query_id=20260428_183347_00004_p2pbr,host=172.20.0.3:10010,component=presto_cpp,snapshot_type=at_start num_tasks=0.0 1777401227128000000
prometheus,query_id=20260428_183347_00004_p2pbr,host=172.20.0.3:10010,component=velox,snapshot_type=at_start cache_shrink_count=0.0 1777401227128000000
```

### At End
```
prometheus,query_id=20260428_183347_00004_p2pbr,host=172.20.0.3:10010,component=presto_cpp,snapshot_type=at_end num_tasks=4.0 1777401565099000000
prometheus,query_id=20260428_183347_00004_p2pbr,host=172.20.0.3:10010,component=velox,snapshot_type=at_end cache_shrink_count=125.0 1777401565099000000
```

## Metrics Categories

### presto_cpp Metrics
Examples:
- `num_tasks`, `num_tasks_running`, `num_tasks_finished`
- `num_http_request`, `num_http_request_error`
- `memory_pushback_count`
- `driver_cpu_executor_queue_size`
- And many more...

### velox Metrics
Examples:
- `cache_shrink_count`
- `memory_cache_num_entries`
- `driver_yield_count`
- `arbitrator_requests_count`
- And many more...

## Build Requirements

To enable InfluxDB support, build with the `influx` tag:

```bash
go build -tags influx
```

Without the tag, the metrics loader will be a no-op (won't write to InfluxDB).

## Troubleshooting

### Metrics Not Appearing in InfluxDB

1. **Check InfluxDB connection**:
   ```bash
   curl -v http://your-influx-url:8086/health
   ```

2. **Verify JSON format**: Ensure your JSON files have the `metrics` section with `snapshots`

3. **Check logs**: Look for warnings about failed InfluxDB writes

4. **Verify bucket**: Ensure the bucket specified in config exists

### No Metrics in JSON

If your JSON files don't have metrics:
- They were generated without `collect_metrics: true`
- Use `pbench run` with metrics collection enabled to generate new files

### Performance Considerations

- Each metric becomes a separate InfluxDB point
- A typical query might generate 100-500 points per worker
- Batch writes are used for efficiency
- 10-second timeout for InfluxDB writes

## Integration with Existing Workflows

### With Event Listener Data

The metrics loader works alongside existing event listener data loading:

```bash
pbench loadjson \
  --influx /path/to/influx.json \
  --mysql /path/to/mysql.json \
  --record-run \
  /path/to/files/*.presto_metrics.json
```

This will:
1. Load query info into MySQL (event listener tables)
2. Load metrics into InfluxDB
3. Record run summary in both databases

### Parallel Processing

The loader supports parallel processing:

```bash
pbench loadjson \
  --influx /path/to/influx.json \
  --parallel 8 \
  /path/to/large/dataset/*.presto_metrics.json
```

## Future Enhancements

Potential improvements:
1. **Metric Filtering**: Only load specific metrics
2. **Aggregation**: Pre-compute deltas and aggregations
3. **Retention Policies**: Automatic downsampling for old data
4. **Custom Tags**: Add custom tags for better organization
5. **Batch Optimization**: Larger batch sizes for better performance

## Related Documentation

- [METRICS_COLLECTION_IMPLEMENTATION.md](METRICS_COLLECTION_IMPLEMENTATION.md) - How metrics are collected during `pbench run`
- [Telegraf Configuration](velox_gpu_stuff/telegraf-worker.conf) - Reference for metric format
- [InfluxDB Documentation](https://docs.influxdata.com/influxdb/v2/)
- [Flux Query Language](https://docs.influxdata.com/flux/v0/)