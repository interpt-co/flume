package processing

import (
	"context"
	"testing"

	"github.com/interpt-co/flume/internal/models"
)

func TestParseWorker_ValidJSON(t *testing.T) {
	msg := models.LogMessage{Content: `{"key":"value"}`}
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsJson {
		t.Error("expected IsJson to be true for valid JSON")
	}
	if string(out.JsonContent) != msg.Content {
		t.Errorf("JsonContent = %s, want %s", out.JsonContent, msg.Content)
	}
}

func TestParseWorker_InvalidJSON(t *testing.T) {
	msg := models.LogMessage{Content: "not json at all"}
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.IsJson {
		t.Error("expected IsJson to be false for invalid JSON")
	}
	if out.JsonContent != nil {
		t.Errorf("expected JsonContent to be nil, got %s", out.JsonContent)
	}
}

func TestParseWorker_EmptyString(t *testing.T) {
	msg := models.LogMessage{Content: ""}
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.IsJson {
		t.Error("expected IsJson to be false for empty string")
	}
	if out.JsonContent != nil {
		t.Errorf("expected JsonContent to be nil, got %s", out.JsonContent)
	}
}

func TestParseWorker_NestedJSON(t *testing.T) {
	nested := `{"outer":{"inner":[1,2,3],"flag":true}}`
	msg := models.LogMessage{Content: nested}
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsJson {
		t.Error("expected IsJson to be true for nested JSON")
	}
	if string(out.JsonContent) != nested {
		t.Errorf("JsonContent = %s, want %s", out.JsonContent, nested)
	}
}

func TestParseWorker_JSONArray(t *testing.T) {
	arr := `[1, "two", null, true]`
	msg := models.LogMessage{Content: arr}
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsJson {
		t.Error("expected IsJson to be true for JSON array")
	}
}

func TestParseWorker_PartialJSON(t *testing.T) {
	msg := models.LogMessage{Content: `{"key": "value"`} // missing closing brace
	out, err := ParseWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.IsJson {
		t.Error("expected IsJson to be false for partial JSON")
	}
}
