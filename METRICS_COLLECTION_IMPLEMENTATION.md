# Metrics Collection Implementation for pbench

## Overview

This implementation adds the ability to collect Presto worker metrics at both query start and end times, storing them in an enhanced JSON format. This mirrors the functionality previously implemented in the Python `metrics_collector.py` script but integrates it directly into pbench.

## Implementation Summary

### New Files Created

1. **`stage/metrics_collector.go`** - Core metrics collection functionality
   - `CollectWorkerMetrics()` - Fetches metrics from all worker nodes
   - `extractWorkerURIs()` - Extracts worker URIs from query info
   - `fetchWorkerMetrics()` - Fetches metrics from a single worker
   - `parsePrometheusMetrics()` - Parses Prometheus text format
   - `metricsToNestedObject()` - Converts metrics to nested structure
   - `FetchQueryInfoAsMap()` - Helper to fetch query info as a map

2. **`stage/metrics_json_writer.go`** - Enhanced JSON output with metrics
   - `saveQueryJsonWithMetrics()` - Saves query JSON with metrics in enhanced format
   - `extractEmbeddedTasks()` - Extracts task data from query info

### Modified Files

1. **`stage/result.go`**
   - Added `MetricsAtStart` and `MetricsAtEnd` fields to `QueryResult` struct

2. **`stage/stage.go`**
   - Added `CollectMetrics` field to `Stage` struct
   - Modified `runQuery()` to collect metrics at start and end times

3. **`stage/stage_utils.go`**
   - Modified `saveQueryJsonFile()` to use enhanced format when metrics are present
   - Added `CollectMetrics` field initialization and inheritance logic

## Enhanced JSON Format

When metrics collection is enabled, the output JSON file will have the following structure:

```json
{
  "query": {
    "queryId": "20260428_183347_00004_p2pbr",
    "queryStats": {
      "executionStartTime": "2026-04-28T18:33:47.128Z",
      "endTime": "2026-04-28T18:39:25.099Z",
      ...
    },
    ...
  },
  "tasks": {
    "172_20_0_3_10010": [...],
    "172_20_0_3_10000": [...]
  },
  "metrics": {
    "start_time": "2026-04-28T18:33:47.128Z",
    "end_time": "2026-04-28T18:39:25.099Z",
    "snapshots": {
      "at_start": {
        "timestamp": "2026-04-28T18:33:47.128Z",
        "workers": {
          "172_20_0_3_10010": {
            "presto_cpp": {
              "num_http_request": 0.0,
              "num_tasks": 0.0,
              ...
            },
            "velox": {
              "cache_shrink_count": 0.0,
              ...
            }
          },
          "172_20_0_3_10000": { ... }
        }
      },
      "at_end": {
        "timestamp": "2026-04-28T18:39:25.099Z",
        "workers": {
          "172_20_0_3_10010": {
            "presto_cpp": {
              "num_http_request": 0.0,
              "num_tasks": 4.0,
              "num_tasks_finished": 3.0,
              ...
            },
            "velox": {
              "cache_shrink_count": 125.0,
              ...
            }
          },
          "172_20_0_3_10000": { ... }
        }
      }
    }
  }
}
```

### Key Features of the Format

1. **Top-level timestamps**: `start_time` and `end_time` for easy reference
2. **Snapshots structure**: Organized into `at_start` and `at_end` sections
3. **Worker-based organization**: Metrics grouped by worker node ID
4. **Nested structure**: Metrics separated into `presto_cpp` and `velox` categories
5. **Extensible**: Can easily add more snapshots (e.g., `at_midpoint`) in the future

## Usage

### Enabling Metrics Collection

Add the `collect_metrics` field to your benchmark stage JSON configuration:

```json
{
  "id": "my_benchmark",
  "collect_metrics": true,
  "query_files": ["queries/*.sql"],
  ...
}
```

### Example Benchmark Configuration

```json
{
  "id": "tpch_with_metrics",
  "description": "TPC-H benchmark with metrics collection",
  "catalog": "hive",
  "schema": "sf100",
  "collect_metrics": true,
  "save_json": true,
  "query_files": ["benchmarks/tpc-h/queries/*.sql"],
  "cold_runs": 1,
  "warm_runs": 2
}
```

### Running a Benchmark

```bash
# Run benchmark with metrics collection enabled
pbench run --output-path ./output benchmarks/my_benchmark.json

# The output will include .presto_metrics.json files with enhanced format
ls output/
# my_benchmark_query1_20260428_183347_00001_xxxxx.presto_metrics.json
# my_benchmark_query2_20260428_183347_00002_xxxxx.presto_metrics.json
```

### Field Inheritance

The `collect_metrics` field follows the same inheritance pattern as other boolean fields like `save_json`:

- If not set in a child stage, it inherits from the parent stage
- Default value is `false` if not specified anywhere
- Can be overridden at any stage level

## How It Works

### Metrics Collection Flow

1. **Query Submission**: Query is submitted to Presto coordinator
2. **Start Metrics Collection** (if enabled):
   - Wait 100ms for query to start executing
   - Fetch query info to identify worker nodes
   - Collect metrics from each worker's `/v1/info/metrics` endpoint
   - Store in `result.MetricsAtStart`
3. **Query Execution**: Query runs normally
4. **End Metrics Collection** (if enabled):
   - After query completes, fetch query info again
   - Collect metrics from each worker
   - Store in `result.MetricsAtEnd`
5. **JSON Output**: If metrics were collected, save in enhanced format

### Metrics Endpoint

Worker metrics are fetched from: `http://<worker-host>:<port>/v1/info/metrics`

This endpoint returns Prometheus text format metrics, which are parsed and organized into the nested structure.

## Benefits Over External Script

1. **Integrated**: No need to run separate post-processing scripts
2. **Atomic**: Metrics are collected as part of the query execution
3. **Consistent**: Uses the same timing and query info as pbench
4. **Efficient**: Reuses existing HTTP client and error handling
5. **Configurable**: Can be enabled/disabled per stage

## Comparison with Python Implementation

| Feature | Python Script | pbench Implementation |
|---------|--------------|----------------------|
| Metrics Collection | Post-processing | Real-time during execution |
| Worker Discovery | From saved JSON | From live query info |
| Timing | Single snapshot | Start + End snapshots |
| Integration | External tool | Built-in |
| Configuration | Command-line | JSON configuration |

## Future Enhancements

Potential improvements for future versions:

1. **Additional Snapshots**: Add `at_midpoint` or periodic snapshots
2. **Metrics Filtering**: Allow specifying which metrics to collect
3. **Aggregation**: Compute deltas between start and end automatically
4. **Retry Logic**: Add retries for failed metrics fetches
5. **Parallel Collection**: Fetch metrics from multiple workers concurrently
6. **Custom Endpoints**: Support additional metrics endpoints beyond `/v1/info/metrics`

## Troubleshooting

### Metrics Not Collected

If metrics are not appearing in the output:

1. Check that `collect_metrics: true` is set in your stage configuration
2. Verify that worker nodes are accessible from the pbench host
3. Check logs for warnings about failed metrics collection
4. Ensure workers expose the `/v1/info/metrics` endpoint

### Empty Metrics

If metrics sections are empty:

1. Query may have completed too quickly (before workers started)
2. Workers may not have any metrics to report yet
3. Check worker logs for errors

### Performance Impact

Metrics collection adds minimal overhead:
- ~100ms delay at query start (to allow query to begin executing)
- HTTP requests to workers (typically <1s total)
- Does not affect query execution itself

## Testing

To test the implementation:

```bash
# Create a simple test benchmark
cat > test_metrics.json <<EOF
{
  "id": "test_metrics",
  "catalog": "hive",
  "schema": "default",
  "collect_metrics": true,
  "save_json": true,
  "queries": ["SELECT 1"]
}
EOF

# Run the benchmark
pbench run --output-path ./test_output test_metrics.json

# Check the output
cat test_output/test_metrics_*.presto_metrics.json | jq '.metrics'
```

Expected output should show the metrics structure with `start_time`, `end_time`, and `snapshots` containing `at_start` and `at_end` sections.