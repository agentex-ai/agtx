//go:build ocr_onnxruntime && cgo

package core

import "testing"

func TestParseONNXOCRCharacterDictFromInferenceYAML(t *testing.T) {
	keys, err := parseONNXOCRCharacterDict(`
PostProcess:
  name: CTCLabelDecode
  character_dict:
  - '!'
  - "A"
  - ''''
  - \
  - 中
PreProcess:
  transform_ops: []
`)
	if err != nil {
		t.Fatalf("parse character dict: %v", err)
	}
	want := []string{"!", "A", "'", "\\", "中"}
	if len(keys) != len(want) {
		t.Fatalf("unexpected key count: got %#v want %#v", keys, want)
	}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("key %d: got %q want %q in %#v", index, keys[index], want[index], keys)
		}
	}
}
