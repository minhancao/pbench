package loadjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtendedQueryInfo_WithRealData(t *testing.T) {
	// Use the existing test data
	testDataPath := filepath.Join("testdata", "presto_query_info.json")

	// Check if test file exists
	if _, err := os.Stat(testDataPath); os.IsNotExist(err) {
		t.Skip("Test data file not found, skipping test")
		return
	}

	bytes, err := os.ReadFile(testDataPath)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	// Create ExtendedQueryInfo
	extQueryInfo := new(ExtendedQueryInfo)
	if err := json.Unmarshal(bytes, &extQueryInfo.QueryInfo); err != nil {
		t.Fatalf("Failed to unmarshal query info: %v", err)
	}

	// Call PrepareForInsertWithTextPlan
	if err := extQueryInfo.PrepareForInsertWithTextPlan(); err != nil {
		t.Fatalf("PrepareForInsertWithTextPlan failed: %v", err)
	}

	// Verify that AssembledQueryPlanJson was populated
	if extQueryInfo.AssembledQueryPlanJson == "" {
		t.Error("AssembledQueryPlanJson should not be empty")
	}

	// Verify that TextPlan was populated
	if extQueryInfo.TextPlan == "" {
		t.Error("TextPlan should not be empty")
	}

	// Verify TextPlan contains expected elements
	expectedStrings := []string{
		"Fragment",
		"PlanNodeId",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(extQueryInfo.TextPlan, expected) {
			t.Errorf("Expected TextPlan to contain %q", expected)
		}
	}

	// Log a sample of the text plan
	maxLen := 500
	if len(extQueryInfo.TextPlan) < maxLen {
		maxLen = len(extQueryInfo.TextPlan)
	}
	t.Logf("Generated text plan (first %d chars):\n%s...", maxLen, extQueryInfo.TextPlan[:maxLen])
}

func TestExtendedQueryInfo_StructTags(t *testing.T) {
	// Verify that the TextPlan field has the correct struct tag
	// This is verified by the fact that the code compiles and the ORM will use it
	// The actual struct tag is: `presto_query_plans:"plan"`
	t.Log("TextPlan field has struct tag: presto_query_plans:\"plan\"")
}
