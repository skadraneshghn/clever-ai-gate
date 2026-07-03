package proxy

import (
	"bytes"
	"testing"

	"github.com/buger/jsonparser"
)

func TestSanitizeSarvamRequest_StripsStreamOptions(t *testing.T) {
	body := []byte(`{"model":"sarvam-105b","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)
	out := sanitizeSarvamRequest(body)

	if bytes.Contains(out, []byte(`"stream_options"`)) {
		t.Fatalf("stream_options should be removed, got: %s", out)
	}
	// Supported fields must survive untouched.
	if v, err := jsonparser.GetString(out, "model"); err != nil || v != "sarvam-105b" {
		t.Fatalf("model field corrupted: %q err=%v", v, err)
	}
	if ok, _ := jsonparser.GetBoolean(out, "stream"); !ok {
		t.Fatalf("stream flag should remain true, got: %s", out)
	}
}

func TestSanitizeSarvamRequest_StripsMultipleUnsupportedFields(t *testing.T) {
	body := []byte(`{"model":"sarvam-30b","messages":[],"logprobs":true,"top_logprobs":5,"service_tier":"auto","user":"u1","stream_options":{"include_usage":true},"logit_bias":{},"suffix":"x","store":false,"metadata":{"a":1},"parallel_tool_calls":true,"temperature":0.5}`)
	out := sanitizeSarvamRequest(body)

	for _, f := range sarvamUnsupportedFields {
		if bytes.Contains(out, []byte(`"`+f+`"`)) {
			t.Fatalf("field %q should be removed, got: %s", f, out)
		}
	}
	// Supported fields must survive.
	if v, err := jsonparser.GetString(out, "model"); err != nil || v != "sarvam-30b" {
		t.Fatalf("model field corrupted: %q err=%v", v, err)
	}
	if temp, err := jsonparser.GetInt(out, "temperature"); err != nil {
		// temperature is a float; GetString is the safe check instead.
		_ = temp
	}
	if v, err := jsonparser.GetString(out, "temperature"); err == nil {
		// temperature may parse as number; just ensure the key is present.
		_ = v
	}
	if !bytes.Contains(out, []byte(`"temperature"`)) {
		t.Fatalf("temperature field should remain, got: %s", out)
	}
}

func TestSanitizeSarvamRequest_PassthroughWhenClean(t *testing.T) {
	body := []byte(`{"model":"sarvam-105b","messages":[{"role":"user","content":"hi"}],"temperature":0.7,"max_tokens":256,"reasoning_effort":"low"}`)
	out := sanitizeSarvamRequest(body)

	// No unsupported fields → must return the exact same slice (no allocation).
	if &body[0] != &out[0] {
		t.Fatalf("clean request should be returned unchanged (same backing array), got: %s", out)
	}
	if !bytes.Equal(body, out) {
		t.Fatalf("clean request should be unchanged, got: %s", out)
	}
}

func TestSanitizeSarvamRequest_EmptyBody(t *testing.T) {
	body := []byte(`{}`)
	out := sanitizeSarvamRequest(body)
	if !bytes.Equal(out, body) {
		t.Fatalf("empty object should be unchanged, got: %s", out)
	}
}
