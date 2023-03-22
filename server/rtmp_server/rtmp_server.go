package rtmp_server

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aler9/gortsplib/v2/pkg/codecs/h264"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/av/avutil"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/deepch/vdk/format/aac"
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
	defer log.Printf("推流关闭: %v", connPath)

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

	go func() {
		cursor := ch.Que.Latest()
		_ = copyPackets(cursor, streams)
	}()

	time.Sleep(time.Second)
	_ = avutil.CopyPackets(ch.Que, conn)
}

// copyPackets 测试服务器保存的数据是否正确
func copyPackets(src av.PacketReader, streams []av.CodecData) (err error) {
	fpH264, err := os.Create("out_server.h264")
	if err != nil {
		log.Println(err)
		return
	}

	defer fpH264.Close()

	fpAAC, err := os.Create("out_server.aac")
	if err != nil {
		log.Println(err)
		return err
	}
	defer fpAAC.Close()
	writerAAC := aac.NewMuxer(fpAAC)

	for _, stream := range streams {
		if stream.Type() == av.AAC {
			_ = writerAAC.WriteHeader([]av.CodecData{stream})
			break
		}
	}

	var annexBNALUStartCode = []byte{0x00, 0x00, 0x00, 0x01}

	for {
		var pkt av.Packet
		if pkt, err = src.ReadPacket(); err != nil {
			log.Println(err)
			break
		}

		// 输出到文件
		{
			stream := streams[pkt.Idx]
			switch stream := stream.(type) {
			case h264parser.CodecData:

				nalUtils, _ := h264parser.SplitNALUs(pkt.Data)
				for _, nal := range nalUtils {
					typ := h264.NALUType(nal[0] & 0x1F)
					if typ == h264.NALUTypeIDR {
						_, _ = fpH264.Write(annexBNALUStartCode)
						_, _ = fpH264.Write(stream.SPS())
						_, _ = fpH264.Write(annexBNALUStartCode)
						_, _ = fpH264.Write(stream.PPS())
					}

					_, _ = fpH264.Write(annexBNALUStartCode)
					_, _ = fpH264.Write(nal)
				}

			case aacparser.CodecData:
				_ = stream
				writerAAC.WritePacket(pkt)
			}
		}

		// dst.WritePacket(pkt)
	}

	return nil
}
