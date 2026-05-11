package security_test

import (
	"testing"

	"github.com/example/ai_gateway/gateway/internal/security"
)

func TestRedactText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "phone", input: "请联系 13812345678", want: "请联系 138XXXX5678"},
		{name: "id card", input: "身份证 110101199001011234", want: "身份证 110101XXXXXX1234"},
		{name: "email", input: "邮箱 alice@example.com", want: "邮箱 a***e@example.com"},
		{name: "idempotent", input: "a***e@example.com", want: "a***e@example.com"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := security.RedactText(tc.input); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSanitizeTextForUpstream(t *testing.T) {
	t.Parallel()

	input := "请联系我 13812345678"
	want := "请联系我 ***"

	if got := security.SanitizeTextForUpstream(input); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRedactTextForDisplay(t *testing.T) {
	t.Parallel()

	input := "请联系我 13812345678"
	want := "请联系我 138XXXX5678"

	if got := security.RedactTextForDisplay(input); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRedactTextCompatibility(t *testing.T) {
	t.Parallel()

	input := "请联系我 13812345678"
	want := "请联系我 138XXXX5678"

	if got := security.RedactText(input); got != want {
		t.Fatalf("expected compatibility result %q, got %q", want, got)
	}
}
