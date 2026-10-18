package process

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestParserVectors(t *testing.T) {
	b, e := os.ReadFile("../../protocol/parser-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Input  string            `json:"input"`
		Values map[string]string `json:"values"`
		Error  bool              `json:"error"`
	}
	if e = json.Unmarshal(b, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		got, e := ParseEnv([]byte(c.Input))
		if (e != nil) != c.Error {
			t.Fatalf("error mismatch for %q: %v", c.Input, e)
		}
		if !c.Error && !reflect.DeepEqual(got, c.Values) {
			t.Fatalf("values for %q: %#v", c.Input, got)
		}
	}
}
func FuzzParseEnv(f *testing.F) {
	f.Add([]byte("A=ok\n"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = ParseEnv(b) })
}
