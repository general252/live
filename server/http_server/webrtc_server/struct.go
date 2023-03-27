package webrtc_server

import (
	"github.com/pion/webrtc/v3"
)

type MethodType string

const (
	Offer     MethodType = "offer"
	Answer    MethodType = "answer"
	Candidate MethodType = "candidate"
)

type JsonProtocol struct {
	Method MethodType                `json:"method"`
	Offer  webrtc.SessionDescription `json:"offer"` // 请求
	Answer JsonAnswer                `json:"answer"`
}

type JsonAnswer struct {
	Code   int                        `json:"code"`             // 错误码
	MSG    string                     `json:"msg"`              // 信息
	Answer *webrtc.SessionDescription `json:"answer,omitempty"` // 回复
}
