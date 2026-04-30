package loadjson

import (
	"pbench/prestoapi"

	"github.com/prestodb/presto-go-client/v2/queryjson"
)

// ExtendedQueryInfo extends queryjson.QueryInfo to add the text plan field
type ExtendedQueryInfo struct {
	queryjson.QueryInfo
	TextPlan string `presto_query_plans:"plan"`
}

// PrepareForInsertWithTextPlan calls the original PrepareForInsert and then generates the text plan
func (e *ExtendedQueryInfo) PrepareForInsertWithTextPlan() error {
	// Call the original PrepareForInsert to populate AssembledQueryPlanJson
	if err := e.QueryInfo.PrepareForInsert(); err != nil {
		return err
	}

	// Generate the text plan from the JSON plan
	if e.AssembledQueryPlanJson != "" {
		textPlan, err := prestoapi.FormatQueryPlanAsText(e.AssembledQueryPlanJson)
		if err != nil {
			// Log the error but don't fail the entire operation
			// The json_plan will still be available
			return err
		}
		e.TextPlan = textPlan
	}

	return nil
}
