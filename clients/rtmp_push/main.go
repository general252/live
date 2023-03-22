package main

import (
	"log"
	"os"
	"time"

	"github.com/aler9/gortsplib/v2/pkg/codecs/h264"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/deepch/vdk/format/aac"
	"github.com/deepch/vdk/format/mp4"
	"github.com/deepch/vdk/format/rtmp"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	fp, err := os.Open("test.mp4")
	if err != nil {
		log.Println(err)
		return
	}
	defer fp.Close()

	deMuxer := mp4.NewDemuxer(fp)
	streams, err := deMuxer.Streams()
	if err != nil {
		log.Println(err)
		return
	}

	for _, stream := range streams {
		log.Println(stream)
	}

	fpH264, err := os.Create("out.h264")
	if err != nil {
		log.Println(err)
		return
	}

	defer fpH264.Close()

	conn, err := rtmp.DialTimeout("rtmp://127.0.0.1/live/test", time.Second*10)
	if err != nil {
		log.Println(err)
		return
	}
	defer func() {
		_ = conn.WriteTrailer()
		time.Sleep(time.Second * 5)
		conn.Close()
	}()

	if err = conn.WriteHeader(streams); err != nil {
		log.Println(err)
		return
	}

	fpAAC, err := os.Create("out.aac")
	if err != nil {
		log.Println(err)
		return
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
		pkt, err := deMuxer.ReadPacket()
		if err != nil {
			log.Println(err)
			break
		}

		stream := streams[pkt.Idx]
		switch stream := stream.(type) {
		case h264parser.CodecData:

			log.Printf("time %v", pkt.Time)
			time.Sleep(time.Millisecond * 20)

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

			if err = conn.WritePacket(pkt); err != nil {
				log.Println(err)
			}

		case aacparser.CodecData:
			_ = stream
			_ = writerAAC.WritePacket(pkt)

			if err = conn.WritePacket(pkt); err != nil {
				log.Println(err)
			}
		}
	}
}
