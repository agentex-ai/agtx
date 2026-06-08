package core

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

type Response struct {
	OK       bool     `json:"ok"`
	Data     any      `json:"data,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    *Error   `json:"error,omitempty"`
	TraceID  string   `json:"trace_id"`
}

func NewResponse(data any, warnings []string) Response {
	return Response{OK: true, Data: data, Warnings: warnings, TraceID: NewTraceID()}
}

func NewErrorResponse(err error, warnings []string) Response {
	return Response{OK: false, Warnings: warnings, Error: ErrorFrom(err), TraceID: NewTraceID()}
}

func NewErrorResponseWithData(err error, data any, warnings []string) Response {
	return Response{OK: false, Data: data, Warnings: warnings, Error: ErrorFrom(err), TraceID: NewTraceID()}
}

func NewTraceID() string {
	var randomBytes [6]byte
	if _, err := rand.Read(randomBytes[:]); err == nil {
		return fmt.Sprintf("agtx-%x-%s", time.Now().UnixNano(), hex.EncodeToString(randomBytes[:]))
	}
	return fmt.Sprintf("agtx-%x", time.Now().UnixNano())
}

func NewSecretToken(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 32
	}
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
