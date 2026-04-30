# Fix for Empty `plan` Column in presto_query_plans Table

## Problem
The `plan` column in the `presto_query_plans` MySQL table was empty for all row entries because the `queryjson.QueryInfo` struct from the external dependency `github.com/prestodb/presto-go-client/v2` did not have a field mapped to the `presto_query_plans:"plan"` struct tag.

## Root Cause
The ORM system in `utils/orm.go` uses struct field tags to map Go struct fields to database columns. The QueryInfo struct only had mappings for:
- `query_id` ✓
- `query` ✓  
- `json_plan` ✓ (via `AssembledQueryPlanJson`)
- `plan` ✗ (no mapping)

## Solution Implemented
Created a text plan formatter that converts the JSON plan to human-readable text format, similar to what Presto's Web UI displays.

### Files Created/Modified:

1. **`prestoapi/plan_formatter.go`** (NEW)
   - Implements `FormatQueryPlanAsText()` function
   - Parses the JSON plan structure
   - Formats it as indented text with plan nodes, operators, and details

2. **`prestoapi/plan_formatter_test.go`** (NEW)
   - Unit tests for the plan formatter
   - Validates the text output format

3. **`cmd/loadjson/query_info_ext.go`** (NEW)
   - Extends `queryjson.QueryInfo` with `ExtendedQueryInfo` struct
   - Adds `TextPlan` field with `presto_query_plans:"plan"` tag
   - Implements `PrepareForInsertWithTextPlan()` method that:
     - Calls original `PrepareForInsert()` to generate JSON plan
     - Converts JSON plan to text format
     - Stores result in `TextPlan` field

4. **`cmd/loadjson/main.go`** (MODIFIED)
   - Changed from `queryjson.QueryInfo` to `ExtendedQueryInfo`
   - Changed `PrepareForInsert()` call to `PrepareForInsertWithTextPlan()`

5. **`cmd/loadjson/query_info_ext_test.go`** (NEW)
   - Tests for the extended QueryInfo functionality

## How It Works

1. When loading a query JSON file, the code now uses `ExtendedQueryInfo` instead of `QueryInfo`
2. After unmarshaling the JSON, `PrepareForInsertWithTextPlan()` is called which:
   - Calls the original `PrepareForInsert()` to populate `AssembledQueryPlanJson`
   - Passes the JSON plan to `FormatQueryPlanAsText()`
   - Stores the formatted text in the `TextPlan` field
3. When inserting into MySQL, the ORM sees the `presto_query_plans:"plan"` tag and inserts the text plan into the `plan` column

## Text Plan Format Example

```
Fragment 0
- Output[PlanNodeId 22][n_name, revenue]
        revenue := sum (4:5)
    - TopN[PlanNodeId 1311][1 by (sum DESC_NULLS_LAST)]
        - TopNPartial[PlanNodeId 1310][1 by (sum DESC_NULLS_LAST)]
            - Aggregate(FINAL)[n_name][PlanNodeId 14]
                    sum := "presto.default.sum"((sum_13)) (4:5)
                - LocalExchange[PlanNodeId 1705][SINGLE] ()
                    - TableScan[PlanNodeId 3][TableHandle {...}]
                            l_orderkey := tpch:l_orderkey (8:5)
                            l_extendedprice := tpch:l_extendedprice (8:5)
```

## Testing

Run the tests to verify:
```bash
# Test the plan formatter
go test ./prestoapi -v -run TestFormatQueryPlanAsText

# Test the extended QueryInfo
go test ./cmd/loadjson -v

# Build the loadjson command
go build ./cmd/loadjson
```

## Usage

The fix is automatically applied when using the `loadjson` command. No changes to command-line arguments or configuration are needed. Simply run:

```bash
./pbench loadjson --run-name my_run --mysql-cfg mysql.json /path/to/query/json/files
```

The `plan` column will now be populated with the human-readable text plan.

## Notes

- The text plan format matches the style shown in Presto's Web UI
- If the JSON plan is empty or invalid, the `plan` column will remain empty (no error thrown)
- The `json_plan` column continues to be populated as before
- Both columns are now available for querying and analysis