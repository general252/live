package server

import (
	"github.com/deepch/vdk/av/pubsub"
	"github.com/deepch/vdk/format"
	"github.com/general252/live/server/http_server"
	"github.com/general252/live/server/rtmp_server"
	"github.com/general252/live/server/rtsp_server"
	"github.com/general252/live/server/server_interface"
	"github.com/general252/live/util"
)

func init() {
	format.RegisterAll()
}

type Option struct {
	RtmpPort int
	HttpPort int

	RtspPort int
	RtpPort  int
	RtcpPort int
}

type Server struct {
	option Option

	channels *util.Map[string, *server_interface.Channel]

	rtmpServer *rtmp_server.RtmpServer
	httpServer *http_server.HttpServer
	rtspServer *rtsp_server.RtspServer
}

func NewServer(option *Option) *Server {
	tis := &Server{
		option: Option{
			RtmpPort: 1935,
			HttpPort: 8080,
		},
		channels: util.NewMap[string, *server_interface.Channel](),
	}

	if option != nil {
		tis.option = *option
	}

	tis.rtmpServer = rtmp_server.NewRtmpServer(tis, tis.option.RtmpPort)
	tis.httpServer = http_server.NewHttpServer(tis, tis.option.HttpPort)
	tis.rtspServer = rtsp_server.NewRtspServer(tis, tis.option.RtspPort, tis.option.RtpPort, tis.option.RtcpPort)

	return tis
}

func (tis *Server) Serve() {

	go func() {
		_ = tis.httpServer.Serve()
	}()

	go func() {
		_ = tis.rtmpServer.Serve()
	}()

	go func() {
		_ = tis.rtspServer.Serve()
	}()
}

func (tis *Server) GetChannel(connPath string) (*server_interface.Channel, bool) {
	ch, ok := tis.channels.Load(connPath)
	return ch, ok
}

func (tis *Server) CreateChannel(connPath string) (*server_interface.Channel, bool) {
	ch, ok := tis.channels.Load(connPath)
	if ok {
		return nil, false
	}

	ch = &server_interface.Channel{}
	ch.Que = pubsub.NewQueue()
	tis.channels.Store(connPath, ch)

	return ch, true
}

func (tis *Server) RemoteChannel(connPath string) {
	if ch, ok := tis.channels.Load(connPath); ok {
		_ = ch.Que.Close()
	}
	tis.channels.Delete(connPath)
}
