#!/usr/bin/env python3
"""
Transform old metrics format to new snapshots format.
Old format: metrics -> worker_id -> {presto_cpp: {...}, velox: {...}}
New format: metrics -> snapshots -> [at_start, at_end] -> worker_id -> {presto_cpp: {...}, velox: {...}}
"""

import json
import sys
from datetime import datetime

def transform_metrics(input_file, output_file):
    # Read the input JSON
    with open(input_file, 'r') as f:
        data = json.load(f)
    
    # Check if metrics section exists
    if 'metrics' not in data:
        print("No metrics section found in input file")
        return
    
    old_metrics = data['metrics']
    
    # Get query start time from query stats
    query_start_time = data['query']['queryStats'].get('executionStartTime', 
                                                        data['query']['queryStats'].get('createTime'))
    query_end_time = data['query']['queryStats'].get('endTime', query_start_time)
    
    # Convert ISO timestamps to milliseconds since epoch
    def iso_to_millis(iso_str):
        if isinstance(iso_str, str):
            dt = datetime.fromisoformat(iso_str.replace('Z', '+00:00'))
            return int(dt.timestamp() * 1000)
        return iso_str
    
    start_timestamp = iso_to_millis(query_start_time)
    end_timestamp = iso_to_millis(query_end_time)
    
    # Create new snapshots structure
    new_metrics = {
        "snapshots": [
            {
                "snapshot_type": "at_start",
                "timestamp_ms": start_timestamp,
                "workers": {}
            },
            {
                "snapshot_type": "at_end", 
                "timestamp_ms": end_timestamp,
                "workers": {}
            }
        ]
    }
    
    # For each worker in old format, create start and end snapshots
    for worker_id, worker_metrics in old_metrics.items():
        # Create "at_start" snapshot (with zeros or low values)
        start_metrics = {
            "presto_cpp": {},
            "velox": {}
        }
        
        # For start metrics, use zeros or minimal values
        for component in ['presto_cpp', 'velox']:
            if component in worker_metrics:
                for metric_name, metric_value in worker_metrics[component].items():
                    # Set start values to 0 for counters, keep same for gauges
                    if 'num_' in metric_name or '_count' in metric_name or 'total_' in metric_name:
                        start_metrics[component][metric_name] = 0.0
                    else:
                        # For non-counter metrics, use same value or 0
                        start_metrics[component][metric_name] = 0.0
        
        # Create "at_end" snapshot (use actual values from old format)
        end_metrics = worker_metrics
        
        # Add to snapshots
        new_metrics["snapshots"][0]["workers"][worker_id] = start_metrics
        new_metrics["snapshots"][1]["workers"][worker_id] = end_metrics
    
    # Replace old metrics with new format
    data['metrics'] = new_metrics
    
    # Write output
    with open(output_file, 'w') as f:
        json.dump(data, f, indent=2)
    
    print(f"Transformed metrics from {input_file} to {output_file}")
    print(f"Start timestamp: {start_timestamp}")
    print(f"End timestamp: {end_timestamp}")
    print(f"Workers: {list(old_metrics.keys())}")

if __name__ == '__main__':
    if len(sys.argv) != 3:
        print("Usage: python3 transform_metrics_to_snapshots.py <input_file> <output_file>")
        sys.exit(1)
    
    transform_metrics(sys.argv[1], sys.argv[2])

# Made with Bob
