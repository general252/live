package rtsp_server

import (
	"github.com/aler9/gortsplib/v2"
	"github.com/aler9/gortsplib/v2/pkg/codecs/mpeg4audio"
	"github.com/aler9/gortsplib/v2/pkg/format"
	"github.com/aler9/gortsplib/v2/pkg/media"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/general252/live/server/server_interface"
	"log"
)

type RtspSessionProxy struct {
	parent   server_interface.ServerInterface
	ctx      *gortsplib.ServerHandlerOnDescribeCtx
	ch       *server_interface.Channel
	connPath string

	stream *gortsplib.ServerStream
}

func NewRtspSessionProxy(parent server_interface.ServerInterface, ctx *gortsplib.ServerHandlerOnDescribeCtx) *RtspSessionProxy {
	tis := &RtspSessionProxy{
		parent:   parent,
		ctx:      ctx,
		connPath: ctx.Path,
	}

	tis.ch, _ = parent.GetChannel(tis.connPath)

	return tis
}

func (tis *RtspSessionProxy) Init() error {
	ch := tis.ch

	streams, err := ch.Que.Latest().Streams()
	if err != nil {
		return err
	}

	var (
		medias    media.Medias
		mediaH264 *media.Media
		mediaAAC  *media.Media

		formatH264 *format.H264
		formatAAC  *format.MPEG4Audio
	)

	for _, stream := range streams {
		switch stream := stream.(type) {
		case h264parser.CodecData:
			log.Printf("%#v", stream.Record)
			formatH264 = &format.H264{
				PayloadTyp:        96,
				SPS:               stream.SPS(),
				PPS:               stream.PPS(),
				PacketizationMode: 1,
			}

			mediaH264 = &media.Media{
				Type:    media.TypeVideo,
				Control: "streamid=0",
				Formats: []format.Format{formatH264},
			}

		case aacparser.CodecData:
			log.Printf("%#v", stream.ConfigBytes)

			formatAAC = &format.MPEG4Audio{
				PayloadTyp: 97,
				Config: &mpeg4audio.Config{
					Type:         mpeg4audio.ObjectType(stream.Config.ObjectType),
					SampleRate:   stream.SampleRate(),
					ChannelCount: 2,
				},
				SizeLength:       13,
				IndexLength:      3,
				IndexDeltaLength: 3,
			}

			if stream.Type() == av.AAC {
			}

			if stream.ChannelLayout() == av.CH_MONO {
				formatAAC.Config.ChannelCount = 1
			} else if stream.ChannelLayout() == av.CH_STEREO {
				formatAAC.Config.ChannelCount = 2
			}

			mediaAAC = &media.Media{
				Type:    media.TypeAudio,
				Control: "streamid=1",
				Formats: []format.Format{formatAAC},
			}
		}
	}

	if mediaH264 != nil {
		medias = append(medias, mediaH264)
	}
	if mediaAAC != nil {
		medias = append(medias, mediaAAC)
	}

	serverStream := gortsplib.NewServerStream(medias)

	// 从rtmp队列中复制packet
	var copyPacket = func() {

		var (
			h264RtpEncoder = formatH264.CreateEncoder()
			aacRtpEncoder  = formatAAC.CreateEncoder()
			packetReader   = ch.Que.Latest()
		)

		for {
			pkt, err := packetReader.ReadPacket()
			if err != nil {
				log.Printf("read packet from rtmp %v", err)
				break
			}

			switch stream := streams[pkt.Idx].(type) {
			case h264parser.CodecData:
				stream.Width()

				// 分割
				nalUtils, _ := h264parser.SplitNALUs(pkt.Data)

				// 打包成rtp
				packets, err := h264RtpEncoder.Encode(nalUtils, pkt.Time)
				if err != nil {
					log.Println(err)
					continue
				}

				for _, packet := range packets {
					serverStream.WritePacketRTP(mediaH264, packet)
				}

			case aacparser.CodecData:
				stream.SampleRate()

				// 打包成rtp
				packets, err := aacRtpEncoder.Encode([][]byte{pkt.Data}, pkt.Time)
				if err != nil {
					log.Println(err)
					continue
				}

				for _, packet := range packets {
					serverStream.WritePacketRTP(mediaAAC, packet)
				}
			}
		}
	}

	go copyPacket()

	tis.stream = serverStream

	return nil
}

func (tis *RtspSessionProxy) Close() {
	log.Printf("RtspSessionProxy Close %v", tis.connPath)

	if tis.stream != nil {
		_ = tis.stream.Close()
	}
}
