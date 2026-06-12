//go:build !ocr_onnxruntime

package core

func builtinOCRAdapterFor(backend string) builtinOCRAdapter {
	return noopBuiltinOCRAdapter{backend: backend}
}
