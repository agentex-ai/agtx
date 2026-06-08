package core

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"
)

const defaultProRequestTimeout = 30 * time.Second

var outboundHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
	CheckRedirect: secureRedirectPolicy,
}

func contextWithDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func secureRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return NewError(CodeInvalidArgument, "too many redirects", nil)
	}
	if req == nil || req.URL == nil {
		return nil
	}
	if req.URL.Scheme == "http" && !isLoopbackHost(req.URL.Hostname()) {
		return NewError(CodeInvalidArgument, "redirect to remote http is not allowed", map[string]any{"url": req.URL.String()})
	}
	if len(via) > 0 && !sameURLOrigin(req.URL, via[0].URL) {
		req.Header.Del("Authorization")
		req.Header.Del("X-AGTX-Device-ID")
	}
	return nil
}

func sameURLOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Scheme == right.Scheme && left.Host == right.Host
}
