package rtsp_server

import (
	"github.com/aler9/gortsplib/v2"
	"github.com/general252/live/server/server_interface"
)

type RtspSession struct {
	connPath string

	pusher *RtspSessionPusher
	proxy  *RtspSessionProxy
}

func NewRtspSession(connPath string) *RtspSession {
	return &RtspSession{connPath: connPath}
}

func (tis *RtspSession) CreatePusher(parent server_interface.ServerInterface, ctx *gortsplib.ServerHandlerOnAnnounceCtx) {
	tis.pusher = NewRtspSessionPusher(parent, ctx)
}

func (tis *RtspSession) CreateProxy(parent server_interface.ServerInterface, ctx *gortsplib.ServerHandlerOnDescribeCtx) error {
	tis.proxy = NewRtspSessionProxy(parent, ctx)
	return tis.proxy.Init()
}

func (tis *RtspSession) GetStream() (*gortsplib.ServerStream, bool) {
	if tis.pusher != nil {
		return tis.pusher.stream, true
	}
	if tis.proxy != nil {
		return tis.proxy.stream, true
	}
	return nil, false
}

func (tis *RtspSession) Close() {
	if tis.pusher != nil {
		tis.pusher.Close()
	}
	if tis.proxy != nil {
		tis.proxy.Close()
	}
}
