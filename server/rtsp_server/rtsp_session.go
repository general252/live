package rtsp_server

import (
	"bytes"
	"encoding/binary"
	"github.com/aler9/gortsplib/v2"
	"github.com/aler9/gortsplib/v2/pkg/codecs/h264"
	"github.com/aler9/gortsplib/v2/pkg/format"
	"github.com/aler9/gortsplib/v2/pkg/formatdecenc/rtph264"
	"github.com/aler9/gortsplib/v2/pkg/media"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/deepch/vdk/codec/opusparser"
	"github.com/general252/live/format/mpegts"
	"github.com/general252/live/server/server_interface"
	"github.com/pion/rtp"
	"log"
	"os"
	"time"
)

// 通过代理示例, rtmp转rtsp
// https://github.com/bluenviron/gortsplib/blob/main/examples/proxy/server.go

type RtspSession struct {
	parent   server_interface.ServerInterface
	ch       *server_interface.Channel
	connPath string

	stream    *gortsplib.ServerStream
	publisher *gortsplib.ServerSession

	multiDecoders  map[format.Format]MultiDecoder
	singleDecoders map[format.Format]SingleDecoder
}

func NewRtspSession(parent server_interface.ServerInterface, ctx *gortsplib.ServerHandlerOnAnnounceCtx) *RtspSession {
	connPath := ctx.Path
	log.Printf("推流: %v", connPath)
	tis := &RtspSession{
		parent:         parent,
		connPath:       connPath,
		stream:         gortsplib.NewServerStream(ctx.Medias),
		publisher:      ctx.Session,
		multiDecoders:  map[format.Format]MultiDecoder{},
		singleDecoders: map[format.Format]SingleDecoder{},
	}

	for _, m := range ctx.Medias {
		for _, f := range m.Formats {
			switch f := f.(type) {
			case *format.H264:
				tis.multiDecoders[f] = f.CreateDecoder()
			case *format.H265:
				tis.multiDecoders[f] = f.CreateDecoder()
			case *format.VP8:
				tis.singleDecoders[f] = f.CreateDecoder()
			case *format.VP9:
				tis.singleDecoders[f] = f.CreateDecoder()
			case *format.Opus:
				tis.singleDecoders[f] = f.CreateDecoder()
			case *format.G711:
				tis.singleDecoders[f] = f.CreateDecoder()
			case *format.G722:
				tis.singleDecoders[f] = f.CreateDecoder()
			case *format.MPEG4Audio:
				tis.multiDecoders[f] = f.CreateDecoder()
			}
		}
	}

	ch, ok := tis.parent.CreateChannel(connPath)
	if !ok {
		log.Printf("CreateChannel fail. %v", connPath)
	} else {
		var streams []av.CodecData

		for _, m := range ctx.Medias {
			for _, f := range m.Formats {
				switch f := f.(type) {
				case *format.H264:
					if codecData, err := h264parser.NewCodecDataFromSPSAndPPS(f.SafeSPS(), f.SafePPS()); err == nil {
						streams = append(streams, codecData)
					} else {
						log.Println(err)
					}
				case *format.Opus:
					streams = append(streams, opusparser.NewCodecData(f.ChannelCount))
				case *format.MPEG4Audio:
					config := aacparser.MPEG4AudioConfig{
						SampleRate:      f.Config.SampleRate,
						ChannelLayout:   av.CH_FRONT_CENTER,
						ObjectType:      uint(f.Config.Type),
						SampleRateIndex: 0,
						ChannelConfig:   0,
					}
					if f.Config.ChannelCount == 2 {
						config.ChannelLayout = av.CH_STEREO
					}

					if codecData, err := aacparser.NewCodecDataFromMPEG4AudioConfig(config); err == nil {
						streams = append(streams, codecData)
					} else {
						log.Println(err)
					}
				}

			}
		}

		ch.Que.WriteHeader(streams)
		tis.ch = ch
	}

	return tis
}

func (tis *RtspSession) onPacketRTP(m *media.Media, f format.Format, pkt *rtp.Packet) {
	switch m.Type {
	case media.TypeVideo:

	case media.TypeAudio:
	}

	switch f := f.(type) {
	case *format.H264:
		tis.onH264(m, f, pkt)
	case *format.H265:
	case *format.Opus:
	case *format.MPEG4Audio:
		tis.onMPEG4Audio(m, f, pkt)
	case *format.VP8:

	case *format.VP9:
	default:
		log.Println("")
	}
}

var (
	mpegTsMuxer *mpegts.Muxer
	fpH264      *os.File
)

func (tis *RtspSession) onH264(m *media.Media, f format.Format, pkt *rtp.Packet) {
	formatH264, ok := f.(*format.H264)
	if !ok {
		return
	}

	decoder, ok := tis.multiDecoders[f]
	if !ok {
		return
	}

	nalus, pts, err := decoder.Decode(pkt)
	if err != nil {
		if err != rtph264.ErrNonStartingPacketAndNoPrevious && err != rtph264.ErrMorePacketsNeeded {
			log.Printf("ERR: %v", err)
		}
		return
	}

	if true {
		for _, nalu := range nalus {
			buf := &bytes.Buffer{}
			if err := binary.Write(buf, binary.BigEndian, int32(len(nalu))); err != nil {
				log.Println(err)
				continue
			}

			buf.Write(nalu)

			typ := h264.NALUType(nalu[0] & 0x1F)
			err = tis.ch.Que.WritePacket(av.Packet{
				IsKeyFrame:      typ == h264.NALUTypeIDR,
				Idx:             0,
				CompositionTime: 0,
				Time:            pts,
				Duration:        0,
				Data:            buf.Bytes(),
			})
			if err != nil {
				log.Println(err)
			}
		}
	}

	// 写ts文件
	if true {
		if mpegTsMuxer == nil {
			if f, err := os.Create("out.ts"); err == nil {
				mpegTsMuxer = mpegts.NewMuxer(f, formatH264.SafeSPS(), formatH264.SafePPS())
			}
		}
		_ = mpegTsMuxer.Encode(nalus, pts)
	}

	// 写h264文件
	if true {
		if fpH264 == nil {
			fpH264, _ = os.Create("out.h264")
		}
		for _, nalu := range nalus {

			typ := h264.NALUType(nalu[0] & 0x1F)
			log.Printf("%10v 0x%02x", typ, nalu[0])

			switch typ {
			case h264.NALUTypeIDR:
				var h264Format *format.H264

				if ok := m.FindFormat(&h264Format); ok {
					fpH264.Write([]byte{0, 0, 0, 1})
					fpH264.Write(h264Format.SafeSPS())
					fpH264.Write([]byte{0, 0, 0, 1})
					fpH264.Write(h264Format.SafePPS())
				}
			}

			fpH264.Write([]byte{0, 0, 0, 1})
			fpH264.Write(nalu)

			_ = av.Packet{
				IsKeyFrame:      false,
				Idx:             0,
				CompositionTime: 0,
				Time:            0,
				Duration:        0,
				Data:            nalu,
			}
		}
	}
}

func (tis *RtspSession) onMPEG4Audio(m *media.Media, f format.Format, pkt *rtp.Packet) {
	formatMPEG4Audio, ok := f.(*format.MPEG4Audio)
	if !ok {
		return
	}

	decoder, ok := tis.multiDecoders[f]
	if !ok {
		return
	}

	_ = formatMPEG4Audio.String()

	nalus, pts, err := decoder.Decode(pkt)
	if err != nil {
		return
	} else {
		_ = pts
	}

	for _, nalu := range nalus {
		_ = nalu
	}
}

func (tis *RtspSession) CloseStream() {
	if tis.stream != nil {
		_ = tis.stream.Close()
		tis.stream = nil
	}
}

func (tis *RtspSession) Close() {
	if tis.stream != nil {
		_ = tis.stream.Close()
		tis.stream = nil
	}
	if tis.publisher != nil {
		_ = tis.publisher.Close()
		tis.publisher = nil
	}

	tis.parent.RemoteChannel(tis.connPath)
}

func (tis *RtspSession) GetConnPath() string {
	return tis.connPath
}

type MultiDecoder interface {
	Decode(pkt *rtp.Packet) ([][]byte, time.Duration, error)
}
type SingleDecoder interface {
	Decode(pkt *rtp.Packet) ([]byte, time.Duration, error)
}
