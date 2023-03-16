package rtmp_server

import (
	"fmt"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"log"

	"github.com/deepch/vdk/av/avutil"
	"github.com/deepch/vdk/format/rtmp"
	"github.com/general252/live/server/server_interface"
)

// ffmpeg -re -i demo.flv -c copy -f flv rtmp://localhost/movie
// ffmpeg -f avfoundation -i "0:0" .... -f flv rtmp://localhost/screen
// ffplay http://localhost:8080/movie
// ffplay http://localhost:8080/screen

type RtmpServer struct {
	server *rtmp.Server
	parent server_interface.ServerInterface
}

func NewRtmpServer(parent server_interface.ServerInterface, port int) *RtmpServer {
	tis := &RtmpServer{
		parent: parent,
	}

	addr := fmt.Sprintf(":%d", port)

	tis.server = &rtmp.Server{
		Addr:          addr,
		HandlePublish: tis.handleRtmpPublish,
		HandlePlay:    tis.handleRtmpPlay,
		HandleConn:    nil,
	}

	return tis
}

func (tis *RtmpServer) Serve() error {
	log.Printf("rtmp listen: %v", tis.server.Addr)
	return tis.server.ListenAndServe()
}

// handleRtmpPlay rtmp play 拉流
func (tis *RtmpServer) handleRtmpPlay(conn *rtmp.Conn) {
	connPath := conn.URL.Path
	log.Printf("拉流: %v", connPath)

	ch, ok := tis.parent.GetChannel(connPath)
	if !ok {
		log.Printf("GetChannel fail. %v", connPath)
		return
	}

	cursor := ch.Que.Latest()
	_ = avutil.CopyFile(conn, cursor)

}

// handleRtmpPublish rtmp publish 推流
func (tis *RtmpServer) handleRtmpPublish(conn *rtmp.Conn) {
	streams, _ := conn.Streams()
	connPath := conn.URL.Path
	log.Printf("推流: %v", connPath)

	ch, ok := tis.parent.CreateChannel(connPath)
	if !ok {
		log.Printf("CreateChannel fail. %v", connPath)
		return
	}
	defer tis.parent.RemoteChannel(connPath)

	for _, stream := range streams {
		switch stream := stream.(type) {
		case h264parser.CodecData:
			log.Printf("%#v", stream.Record)
		case aacparser.CodecData:
			log.Printf("%#v", stream.ConfigBytes)
		}
	}
	_ = ch.Que.WriteHeader(streams)

	_ = avutil.CopyPackets(ch.Que, conn)
}
