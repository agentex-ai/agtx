package core

type ProSchemeResult struct {
	Scheme  string `json:"scheme"`
	Command string `json:"command,omitempty"`
}

var proRegisterSchemeHook func() (ProSchemeResult, error)

func SwapProRegisterSchemeHookForTest(hook func() (ProSchemeResult, error)) func() {
	previous := proRegisterSchemeHook
	proRegisterSchemeHook = hook
	return func() {
		proRegisterSchemeHook = previous
	}
}
