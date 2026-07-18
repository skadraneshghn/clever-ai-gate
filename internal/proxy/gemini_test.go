package proxy

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSanitizeGeminiToolSchema_StripsTopLevelDollarSchema(t *testing.T) {
	in := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"path":{"type":"string"}}}`)
	out := sanitizeGeminiToolSchema(in)
	if bytes.Contains(out, []byte("$schema")) {
		t.Errorf("expected $schema stripped, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"type":"object"`)) {
		t.Errorf("expected structural keys preserved, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"path"`)) {
		t.Errorf("expected nested properties preserved, got %s", out)
	}
}

func TestSanitizeGeminiToolSchema_StripsExclusiveBoundsKeepsMinimum(t *testing.T) {
	// This is the exact upstream rejection reported by Google:
	//   Unknown name "exclusiveMinimum" at 'tools[0].function_declarations[i].parameters...'
	in := json.RawMessage(`{"type":"number","minimum":0,"maximum":100,"exclusiveMinimum":0,"exclusiveMaximum":100}`)
	out := sanitizeGeminiToolSchema(in)
	if bytes.Contains(out, []byte("exclusiveMinimum")) || bytes.Contains(out, []byte("exclusiveMaximum")) {
		t.Errorf("expected exclusiveMinimum/exclusiveMaximum stripped, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"minimum":0`)) || !bytes.Contains(out, []byte(`"maximum":100`)) {
		t.Errorf("expected minimum/maximum preserved, got %s", out)
	}
}

func TestSanitizeGeminiToolSchema_StripsExclusiveBoundsNested(t *testing.T) {
	in := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"exclusiveMinimum":1},"tags":{"type":"array","items":{"type":"string","minLength":1,"patternProperties":{"^[a-z]+$":{}}}}}}`)
	out := sanitizeGeminiToolSchema(in)
	if bytes.Contains(out, []byte("exclusiveMinimum")) {
		t.Errorf("expected nested exclusiveMinimum stripped, got %s", out)
	}
	if bytes.Contains(out, []byte("patternProperties")) {
		t.Errorf("expected nested patternProperties stripped, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"minimum":1`)) {
		t.Errorf("expected nested minimum preserved, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"minLength":1`)) {
		t.Errorf("expected nested minLength preserved, got %s", out)
	}
}

func TestSanitizeGeminiToolSchema_StripsNestedDollarSchema(t *testing.T) {
	in := json.RawMessage(`{"type":"object","properties":{"cfg":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"x":{"type":"string"}}}}}`)
	out := sanitizeGeminiToolSchema(in)
	if bytes.Contains(out, []byte("$schema")) {
		t.Errorf("expected nested $schema stripped, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"x"`)) {
		t.Errorf("expected deeply nested properties preserved, got %s", out)
	}
}

func TestSanitizeGeminiToolSchema_PassthroughNonObject(t *testing.T) {
	cases := []string{`null`, `true`, `42`, `"string"`, ``, `not-json`}
	for _, c := range cases {
		in := json.RawMessage(c)
		out := sanitizeGeminiToolSchema(in)
		if string(out) != c {
			t.Errorf("expected %q unchanged, got %q", c, string(out))
		}
	}
}

func TestSanitizeGeminiToolSchema_NoChangeWhenAbsent(t *testing.T) {
	in := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	out := sanitizeGeminiToolSchema(in)
	// Key order may be normalized by the marshal round-trip; compare structurally.
	var a, b map[string]any
	if err := json.Unmarshal(in, &a); err != nil {
		t.Fatalf("unmarshal in: %v", err)
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("unmarshal out: %v", err)
	}
	if len(a) != len(b) {
		t.Errorf("expected no keys removed, got in=%v out=%v", a, b)
	}
	if _, ok := b["$schema"]; ok {
		t.Errorf("did not expect $schema in output, got %s", out)
	}
}

func TestTranspileOpenAIToGemini_EmptyToolsOmitted(t *testing.T) {
	body := []byte(`{"model":"gemini-3.5-flash","messages":[{"role":"user","content":"hi"}],"tools":[]}`)
	out, err := transpileOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("transpile failed: %v", err)
	}
	if bytes.Contains(out, []byte(`"tools"`)) {
		t.Errorf("expected tools field omitted for empty tools array, got %s", out)
	}
	if bytes.Contains(out, []byte("functionDeclarations")) {
		t.Errorf("expected no functionDeclarations for empty tools array, got %s", out)
	}
}

func TestTranspileOpenAIToGemini_StripsDollarSchemaFromToolParams(t *testing.T) {
	body := []byte(`{"model":"gemini-3.5-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read_file","description":"read","parameters":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"path":{"type":"string"}}}}}]}`)
	out, err := transpileOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("transpile failed: %v", err)
	}
	if bytes.Contains(out, []byte("$schema")) {
		t.Errorf("expected $schema stripped from tool parameters, got %s", out)
	}
	if !bytes.Contains(out, []byte("read_file")) {
		t.Errorf("expected function name preserved, got %s", out)
	}
	if !bytes.Contains(out, []byte("functionDeclarations")) {
		t.Errorf("expected functionDeclarations emitted for real tool, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"path"`)) {
		t.Errorf("expected tool parameter properties preserved, got %s", out)
	}
}

func TestTranspileOpenAIToGemini_StripsExclusiveBoundsFromToolParams(t *testing.T) {
	// Reproduces the reported Google 400: "Unknown name \"exclusiveMinimum\" at
	// 'tools[0].function_declarations[2].parameters...': Cannot find field."
	body := []byte(`{"model":"gemini-3.5-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"set_temp","description":"set temperature","parameters":{"type":"object","properties":{"value":{"type":"number","minimum":0,"maximum":2,"exclusiveMinimum":0,"exclusiveMaximum":2}}}}}]}`)
	out, err := transpileOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("transpile failed: %v", err)
	}
	if bytes.Contains(out, []byte("exclusiveMinimum")) || bytes.Contains(out, []byte("exclusiveMaximum")) {
		t.Errorf("expected exclusiveMinimum/exclusiveMaximum stripped, got %s", out)
	}
	if !bytes.Contains(out, []byte(`"minimum":0`)) || !bytes.Contains(out, []byte(`"maximum":2`)) {
		t.Errorf("expected minimum/maximum preserved for Google, got %s", out)
	}
	if !bytes.Contains(out, []byte("set_temp")) {
		t.Errorf("expected function name preserved, got %s", out)
	}
}

func TestTranspileOpenAIToGemini_NonFunctionToolsOmitted(t *testing.T) {
	// A tools array containing only a non-"function" entry must not produce an
	// empty functionDeclarations list (Google rejects that).
	body := []byte(`{"model":"gemini-3.5-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"other","function":{"name":"x"}}]}`)
	out, err := transpileOpenAIToGemini(body)
	if err != nil {
		t.Fatalf("transpile failed: %v", err)
	}
	if bytes.Contains(out, []byte("functionDeclarations")) {
		t.Errorf("expected no functionDeclarations for non-function tool, got %s", out)
	}
}
