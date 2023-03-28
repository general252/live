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

type JsonRequest struct {
	Method MethodType         `json:"method"`
	Data   JsonRequestPayload `json:"data"`
}

type JsonRequestPayload struct {
	Offer *webrtc.SessionDescription `json:"offer"` // 请求
	// Candidate *webrtc.ICECandidate       `json:"candidate"` // 候选数据
}

type JsonResponse struct {
	Method MethodType          `json:"method"`
	Code   int                 `json:"code"` // 错误码
	Msg    string              `json:"msg"`  // 信息
	Data   JsonResponsePayload `json:"data"`
}

type JsonResponsePayload struct {
	Answer *webrtc.SessionDescription `json:"answer,omitempty"` // 回复
}
