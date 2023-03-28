package http_server

import (
	"fmt"
	"github.com/general252/live/server/http_server/httflv_server"
	"github.com/general252/live/server/http_server/webrtc_server"
	"github.com/general252/live/server/server_interface"
	"github.com/gin-gonic/gin"
	"log"
)

type HttpServer struct {
	parent server_interface.ServerInterface
	port   int
}

func NewHttpServer(parent server_interface.ServerInterface, port int) *HttpServer {
	return &HttpServer{
		parent: parent,
		port:   port,
	}
}

func (tis *HttpServer) Serve() error {
	// http
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	var (
		httFlvServer = httflv_server.NewHttpFlvServer(tis.parent)
		webrtcServer = webrtc_server.NewWebrtcServer(tis.parent)
	)

	r.StaticFS("/home", gin.Dir("./static/ui", true))
	r.GET("/httpflv/:ConnPath", httFlvServer.OnHttpFLV)
	r.GET("/webrtc/pusher/:ConnPath", webrtcServer.OnPusher)
	r.GET("/webrtc/player/:ConnPath", webrtcServer.OnPlayer)

	// 启动http服务
	addr := fmt.Sprintf(":%v", tis.port)
	log.Printf("http listen: %v", addr)
	return r.Run(addr)
}
