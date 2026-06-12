//go:build ocr_onnxruntime && !cgo

package core

func builtinOCRAdapterFor(backend string) builtinOCRAdapter {
	return noopBuiltinOCRAdapter{backend: backend}
}
