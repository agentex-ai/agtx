package core

import "context"

type noopBuiltinOCRAdapter struct {
	backend string
}

func (a noopBuiltinOCRAdapter) Linked() bool {
	return false
}

func (a noopBuiltinOCRAdapter) Probe(context.Context, builtinOCRConfig) builtinOCRAdapterProbe {
	return builtinOCRAdapterProbe{Error: "native OCR adapter is not linked into this build"}
}

func (a noopBuiltinOCRAdapter) Run(context.Context, builtinOCRConfig, builtinOCRRequest) (RunResult, error) {
	return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "native OCR backend is configured but this build has no linked inference adapter", map[string]any{"backend": a.backend, "no_python": true})
}
