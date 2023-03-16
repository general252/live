package rtsp_server

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aler9/gortsplib/v2"
	"github.com/aler9/gortsplib/v2/pkg/base"
	"github.com/aler9/gortsplib/v2/pkg/codecs/mpeg4audio"
	"github.com/aler9/gortsplib/v2/pkg/format"
	"github.com/aler9/gortsplib/v2/pkg/media"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/general252/live/server/server_interface"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
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

	sessionMutex sync.RWMutex
	sessionMap   map[string]*RtspSession

	mutex     sync.Mutex
	stream    *gortsplib.ServerStream
	publisher *gortsplib.ServerSession
}

func newServerHandler(parent server_interface.ServerInterface) *serverHandler {
	return &serverHandler{
		parent:     parent,
		sessionMap: map[string]*RtspSession{},
	}
}

// OnConnOpen called when a connection is opened.
func (sh *serverHandler) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	log.Printf("conn opened")
}

// OnConnClose called when a connection is closed.
func (sh *serverHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	log.Printf("conn closed (%v)", ctx.Error)
}

// OnSessionOpen called when a session is opened.
func (sh *serverHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Printf("session opened")
}

// OnSessionClose called when a session is closed.
func (sh *serverHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Printf("session closed")

	sh.sessionMutex.Lock()
	defer sh.sessionMutex.Unlock()

	for _, session := range sh.sessionMap {
		if ctx.Session == session.publisher {
			session.CloseStream()
			delete(sh.sessionMap, session.GetConnPath())

			break
		}
	}
}

// OnDescribe called when receiving a DESCRIBE request.
func (sh *serverHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	connPath := ctx.Path
	log.Printf("describe request %v", connPath)

	sh.sessionMutex.Lock()
	defer sh.sessionMutex.Unlock()

	session, ok := sh.sessionMap[connPath]
	if !ok {
		// TODO:
		if ch, ok := sh.parent.GetChannel(connPath); ok {
			if streams, err := ch.Que.Latest().Streams(); err == nil {
				var medias media.Medias
				for _, stream := range streams {
					switch stream := stream.(type) {
					case h264parser.CodecData:
						log.Printf("%#v", stream.Record)
						medias = append(medias, &media.Media{
							Type:    media.TypeVideo,
							Control: "streamid=0",
							Formats: []format.Format{&format.H264{
								PayloadTyp:        96,
								SPS:               stream.SPS(),
								PPS:               stream.PPS(),
								PacketizationMode: 1,
							}},
						})
					case aacparser.CodecData:
						log.Printf("%#v", stream.ConfigBytes)

						f := &format.MPEG4Audio{
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
							f.Config.ChannelCount = 1
						} else if stream.ChannelLayout() == av.CH_STEREO {
							f.Config.ChannelCount = 2
						}

						medias = append(medias, &media.Media{
							Type:    media.TypeAudio,
							Control: "streamid=1",
							Formats: []format.Format{f},
						})
					}
				}

				stream := gortsplib.NewServerStream(medias)

				go func() {
					reader := ch.Que.Latest()

					var (
						clockRate  = float64(90000)
						packetizer = rtp.NewPacketizer(
							1200,
							96,
							222,
							&codecs.H264Payloader{},
							rtp.NewRandomSequencer(),
							uint32(clockRate),
						)

						samples = uint32((time.Millisecond * 40).Seconds() * clockRate)
					)

					for {
						pkt, err := reader.ReadPacket()
						if err != nil {
							break
						}

						if pkt.Idx == 0 {
							nalus, _ := h264parser.SplitNALUs(pkt.Data)
							for _, nalu := range nalus {
								packets := packetizer.Packetize(nalu, samples)
								for _, packet := range packets {
									stream.WritePacketRTP(medias[0], packet)
								}
							}
						} else if pkt.Idx == 1 {

						}

					}
				}()

				return &base.Response{
					StatusCode: base.StatusOK,
				}, stream, nil
			}
		}

		log.Println("not found session ", connPath)
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	// send medias that are being published to the client
	return &base.Response{
		StatusCode: base.StatusOK,
	}, session.stream, nil
}

// OnAnnounce called when receiving an ANNOUNCE request.
func (sh *serverHandler) OnAnnounce(ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	connPath := ctx.Path
	log.Printf("announce request")

	sh.sessionMutex.Lock()
	defer sh.sessionMutex.Unlock()

	if session, ok := sh.sessionMap[connPath]; ok {
		session.Close()
		delete(sh.sessionMap, ctx.Path)
	}

	session := NewRtspSession(sh.parent, ctx)

	// save the track list and the publisher
	sh.sessionMap[connPath] = session

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnSetup called when receiving a SETUP request.
func (sh *serverHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Printf("setup request")

	sh.sessionMutex.RLock()
	defer sh.sessionMutex.RUnlock()

	session, ok := sh.sessionMap[ctx.Path]
	if !ok {
		log.Println("not found session ", ctx.Path)
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, session.stream, nil
}

// OnPlay called when receiving a PLAY request.
func (sh *serverHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	log.Printf("play request")

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnRecord called when receiving a RECORD request.
func (sh *serverHandler) OnRecord(ctx *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	log.Printf("record request")

	sh.sessionMutex.RLock()
	defer sh.sessionMutex.RUnlock()

	// if we are the publisher, route the RTP packet to all readers
	for _, session := range sh.sessionMap {
		if ctx.Session == session.publisher {

			// called when receiving a RTP packet
			session.publisher.OnPacketRTPAny(func(medi *media.Media, forma format.Format, pkt *rtp.Packet) {
				// route the RTP packet to all readers
				session.stream.WritePacketRTP(medi, pkt)
				session.onPacketRTP(medi, forma, pkt) // 转给webrtc
			})

			break
		}
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}
