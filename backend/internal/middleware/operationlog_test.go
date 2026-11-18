package middleware

import (
	"strings"
	"testing"
)

func TestRedactJSONRedactsNestedSensitiveFields(t *testing.T) {
	got := redactJSON([]byte(`{"user":{"password":"p@ss","profile":{"token":"t"}},"items":[{"secret":"s"}]}`))

	for _, leaked := range []string{"p@ss", `"t"`, `"s"`} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted body still contains %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"password":"***"`) || !strings.Contains(got, `"token":"***"`) {
		t.Fatalf("sensitive fields were not masked: %s", got)
	}
}

func TestRedactJSONDoesNotStoreTruncatedBody(t *testing.T) {
	got := redactJSON([]byte(`{"password":"secret-but-cut-off`))

	if strings.Contains(got, "secret-but-cut-off") {
		t.Fatalf("truncated body leaked into audit log: %s", got)
	}
	if got != "[非 JSON 或已截断的请求体，已省略]" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactJSONKeepsOrdinaryBody(t *testing.T) {
	got := redactJSON([]byte(`{"name":"alice","enabled":true}`))

	if !strings.Contains(got, `"name":"alice"`) || !strings.Contains(got, `"enabled":true`) {
		t.Fatalf("ordinary body was altered: %s", got)
	}
}
