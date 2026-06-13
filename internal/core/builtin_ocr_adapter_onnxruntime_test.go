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

func TestONNXOCRRecognitionDynamicWidth(t *testing.T) {
	info := &builtinOCRModelInfo{Inputs: []builtinOCRTensorInfo{{Dimensions: []int64{-1, 3, 48, -1}}}}
	height, width, dynamic := onnxOCRRecognitionSize(info, builtinOCRSettings{})
	if height != 48 || width != 320 || !dynamic {
		t.Fatalf("unexpected dynamic recognition size: height=%d width=%d dynamic=%v", height, width, dynamic)
	}
	short := onnxOCRRecognitionWidthForBox(onnxOCRBox{X1: 0, Y1: 0, X2: 120, Y2: 48}, height, width, dynamic, builtinOCRSettings{})
	if short != 320 {
		t.Fatalf("short box should keep base width, got %d", short)
	}
	long := onnxOCRRecognitionWidthForBox(onnxOCRBox{X1: 0, Y1: 0, X2: 1800, Y2: 48}, height, width, dynamic, builtinOCRSettings{RecMaxWidth: 640})
	if long != 640 {
		t.Fatalf("long box should clamp to rec max width, got %d", long)
	}
	overridden := onnxOCRRecognitionWidthForBox(onnxOCRBox{X1: 0, Y1: 0, X2: 1800, Y2: 48}, height, width, dynamic, builtinOCRSettings{RecWidth: 512})
	if overridden != 512 {
		t.Fatalf("rec width override should win, got %d", overridden)
	}
}

func TestONNXOCRRecognitionFixedWidth(t *testing.T) {
	info := &builtinOCRModelInfo{Inputs: []builtinOCRTensorInfo{{Dimensions: []int64{-1, 3, 48, 320}}}}
	height, width, dynamic := onnxOCRRecognitionSize(info, builtinOCRSettings{})
	if height != 48 || width != 320 || dynamic {
		t.Fatalf("unexpected fixed recognition size: height=%d width=%d dynamic=%v", height, width, dynamic)
	}
	got := onnxOCRRecognitionWidthForBox(onnxOCRBox{X1: 0, Y1: 0, X2: 1800, Y2: 48}, height, width, dynamic, builtinOCRSettings{})
	if got != 320 {
		t.Fatalf("fixed width model should keep model width, got %d", got)
	}
}
