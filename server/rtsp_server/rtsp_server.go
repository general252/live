package rtsp_server

import (
	"fmt"
	"github.com/aler9/gortsplib/v2"
	"github.com/aler9/gortsplib/v2/pkg/base"
	"github.com/aler9/gortsplib/v2/pkg/format"
	"github.com/aler9/gortsplib/v2/pkg/media"
	"github.com/general252/live/server/server_interface"
	"github.com/general252/live/util"
	"github.com/pion/rtp"
	"log"
)

// ffmpeg -re -i demo.flv -c:v libx264 -c:a aac -f rtsp rtsp://127.0.0.1:554/test
// ffplay rtsp://127.0.0.1:554/test

type RtspServer struct {
	parent server_interface.ServerInterface
	server *gortsplib.Server
}

func NewRtspServer(parent server_interface.ServerInterface, rtspPort, rtpPort, rtcpPort int) *RtspServer {
	tis := &RtspServer{
		parent: parent,
		server: &gortsplib.Server{
			Handler:           newServerHandler(parent),
			RTSPAddress:       ":8554",
			UDPRTPAddress:     ":8000",
			UDPRTCPAddress:    ":8001",
			MulticastIPRange:  "224.1.0.0/16",
			MulticastRTPPort:  8002,
			MulticastRTCPPort: 8003,
		},
	}

	tis.server.RTSPAddress = fmt.Sprintf(":%v", rtspPort)
	tis.server.UDPRTPAddress = fmt.Sprintf(":%v", rtpPort)
	tis.server.UDPRTCPAddress = fmt.Sprintf(":%v", rtcpPort)

	return tis
}

func (tis *RtspServer) Serve() error {
	log.Printf("rtsp listen: %v", tis.server.RTSPAddress)
	return tis.server.StartAndWait()
}

// This example shows how to
// 1. create a RTSP server which accepts plain connections
// 2. allow a single client to publish a stream with TCP or UDP
// 3. allow multiple clients to read that stream with TCP, UDP or UDP-multicast

type serverHandler struct {
	parent server_interface.ServerInterface

	sessions *util.Map[string, *RtspSession]
}

func newServerHandler(parent server_interface.ServerInterface) *serverHandler {
	return &serverHandler{
		parent:   parent,
		sessions: util.NewMap[string, *RtspSession](),
	}
}

// OnConnOpen called when a connection is opened.
func (sh *serverHandler) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	log.Printf("conn opened")
}

// OnConnClose called when a connection is closed.
func (sh *serverHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	log.Printf("conn closed (%v)", ctx.Error)

	userData := ctx.Conn.UserData()
	if userData != nil {
		if session, ok := userData.(*RtspSession); ok {
			session.Close()
			sh.sessions.Delete(session.connPath)
		}
	}
}

// OnSessionOpen called when a session is opened.
func (sh *serverHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Printf("session opened")
}

// OnSessionClose called when a session is closed.
func (sh *serverHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Printf("session closed")
}

// OnDescribe called when receiving a DESCRIBE request.
func (sh *serverHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	connPath := ctx.Path
	log.Printf("describe request %v", connPath)

	// 拉流

	session, ok := sh.sessions.Load(connPath)
	if !ok {
		if _, ok = sh.parent.GetChannel(connPath); ok {
			// 通过代理拉流
			sess := NewRtspSession(connPath)

			// 初始化失败
			if err := sess.CreateProxy(sh.parent, ctx); err != nil {
				log.Println(err)

				return &base.Response{
					StatusCode: base.StatusBadGateway,
				}, nil, nil
			}

			sh.sessions.Store(connPath, sess)

			// 绑定userData
			ctx.Conn.SetUserData(sess)

			session, ok = sh.sessions.Load(connPath)
		}
	}

	if !ok {
		log.Println("not found session ", connPath)
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	stream, ok := session.GetStream()

	// send medias that are being published to the client
	return &base.Response{
		StatusCode: base.StatusOK,
	}, stream, nil
}

// OnAnnounce called when receiving an ANNOUNCE request.
func (sh *serverHandler) OnAnnounce(ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	connPath := ctx.Path
	log.Printf("announce request, %v", connPath)

	// 推流

	// 关闭已有的
	if session, ok := sh.sessions.Load(connPath); ok {
		session.Close()
		sh.sessions.Delete(connPath)
	}

	// 创建新的推流
	session := NewRtspSession(connPath)
	session.CreatePusher(sh.parent, ctx)

	// save the track list and the publisher
	sh.sessions.Store(connPath, session)

	// 绑定userData
	ctx.Conn.SetUserData(session)

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnSetup called when receiving a SETUP request.
func (sh *serverHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	connPath := ctx.Path
	log.Printf("setup request, %v", connPath)

	session, ok := sh.sessions.Load(connPath)
	if !ok {
		log.Println("not found session ", connPath)
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	stream, ok := session.GetStream()

	return &base.Response{
		StatusCode: base.StatusOK,
	}, stream, nil
}

// OnPlay called when receiving a PLAY request.
func (sh *serverHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	connPath := ctx.Path
	log.Printf("play request, %v", connPath)

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnRecord called when receiving a RECORD request.
func (sh *serverHandler) OnRecord(ctx *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	connPath := ctx.Path
	log.Printf("record request, %v", connPath)

	// if we are the publisher, route the RTP packet to all readers
	var session *RtspSession
	sh.sessions.Range(func(key string, value *RtspSession) bool {
		if value.pusher == nil {
			return true
		}

		if ctx.Session == value.pusher.publisher {
			session = value
			return false
		}
		return true
	})

	if session != nil {
		// called when receiving a RTP packet
		session.pusher.publisher.OnPacketRTPAny(func(medi *media.Media, forma format.Format, pkt *rtp.Packet) {
			// route the RTP packet to all readers
			session.pusher.stream.WritePacketRTP(medi, pkt)
			session.pusher.onPacketRTP(medi, forma, pkt) // 转给webrtc
		})
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}
