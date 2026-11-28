package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestJSONContract(t *testing.T) {
	for _, tc := range []struct {
		args []string
		exit int
	}{{[]string{"version", "--json"}, 0}, {[]string{"template", "--list", "--json"}, 0}, {[]string{"ocr", "--json"}, 2}, {[]string{"redact", "missing-test-image", "--json"}, 2}} {
		var out, diag bytes.Buffer
		code := Run(context.Background(), tc.args, "vTest", nil, &out, &diag)
		var r response
		if code != tc.exit || !json.Valid(out.Bytes()) || json.Unmarshal(out.Bytes(), &r) != nil || r.OK != (code == 0) || r.Command != tc.args[0] || diag.Len() != 0 {
			t.Errorf("%s: exit %d, out %s, stderr %s", tc.args, code, &out, &diag)
		}
	}
}
