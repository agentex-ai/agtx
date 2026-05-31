//go:build !windows && !darwin

package core

func registerProScheme() (ProSchemeResult, error) {
	return ProSchemeResult{}, NewError(CodeInvalidArgument, "pro scheme registration is only automated on macOS and Windows", map[string]any{"scheme": proSchemeName})
}
