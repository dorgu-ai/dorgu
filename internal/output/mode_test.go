package output

import "testing"

func TestInitJSON(t *testing.T) {
	Init(true, false)
	if !IsJSON() {
		t.Error("expected JSON mode when json flag is true")
	}
	if GetMode() != ModeJSON {
		t.Errorf("expected ModeJSON, got %d", GetMode())
	}
}

func TestInitNoColor(t *testing.T) {
	Init(false, true)
	if IsJSON() {
		t.Error("should not be JSON mode with only no-color flag")
	}
}

func TestPrintJSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	if err := PrintJSON(data); err != nil {
		t.Errorf("PrintJSON failed: %v", err)
	}
}

func TestPrintJSONLine(t *testing.T) {
	data := map[string]string{"key": "value"}
	if err := PrintJSONLine(data); err != nil {
		t.Errorf("PrintJSONLine failed: %v", err)
	}
}
