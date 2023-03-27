package webrtc_server

import (
	"fmt"
	"github.com/deepch/vdk/av"
	"github.com/deepch/vdk/av/pubsub"
	"github.com/deepch/vdk/codec/aacparser"
	"github.com/deepch/vdk/codec/h264parser"
	"github.com/deepch/vdk/codec/h265parser"
	"github.com/deepch/vdk/codec/opusparser"
	"github.com/general252/live/server/server_interface"
	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"log"
	"time"
)

type Proxy struct {
	connPath            string
	websocketConnection *websocket.Conn
	peerConnection      *webrtc.PeerConnection

	sourceChannel *server_interface.Channel
	cursor        *pubsub.QueueCursor

	tracksSample []*webrtc.TrackLocalStaticSample
}

func NewProxy(connPath string, conn *websocket.Conn, sourceChannel *server_interface.Channel) *Proxy {
	return &Proxy{
		connPath:            connPath,
		websocketConnection: conn,
		sourceChannel:       sourceChannel,
		cursor:              sourceChannel.Que.Latest(),
	}
}

func (tis *Proxy) Close() error {
	if tis.peerConnection != nil {
		_ = tis.peerConnection.Close()
	}

	if tis.websocketConnection != nil {
		_ = tis.websocketConnection.Close()
	}

	return nil
}

// OnOfferSample 拉流请求
func (tis *Proxy) OnOfferSample(request *JsonProtocol, api *webrtc.API, tracks []*webrtc.TrackLocalStaticSample) error {
	if tis.peerConnection != nil {
		_ = tis.peerConnection.Close()
		tis.peerConnection = nil
	}

	var wsConnection = tis.websocketConnection

	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		return err
	}

	if err = tis.initPeerConnection(peerConnection); err != nil {
		return err
	}

	// 添加流 AddTrack
	for _, rtpTracker := range tracks {
		if _, err = peerConnection.AddTrack(rtpTracker); err != nil {
			return err
		}
	}

	// Set the remoteWebrtc SessionDescription
	if err = peerConnection.SetRemoteDescription(request.Offer); err != nil {
		return err
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return err
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	tis.tracksSample = tracks

	go tis.run()

	err = wsConnection.WriteJSON(&JsonProtocol{
		Method: Answer,
		Answer: JsonAnswer{
			Code:   0,
			MSG:    "success",
			Answer: peerConnection.LocalDescription(),
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (tis *Proxy) initPeerConnection(peerConnection *webrtc.PeerConnection) error {

	// ICE状态改变
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("[ice state] Connection State has changed %s \n", connectionState.String())

		if connectionState == webrtc.ICEConnectionStateFailed {
			if closeErr := peerConnection.Close(); closeErr != nil {
				log.Println(closeErr)
			}
		}
	})

	// ICE
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			log.Printf("打洞数据 [ice] OnICECandidate %s \n", candidate.String())
		}
	})

	// track
	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			ticker := time.NewTicker(time.Second * 5)
			defer ticker.Stop()

			for range ticker.C {
				err := peerConnection.WriteRTCP(
					[]rtcp.Packet{
						&rtcp.PictureLossIndication{
							MediaSSRC: uint32(remoteTrack.SSRC()),
						},
					},
				)

				if err != nil {
					fmt.Println(err)
					break
				}
			}
		}()
	})

	return nil
}

func (tis *Proxy) OnCandidate(request *JsonProtocol) error {
	return nil
}

func (tis *Proxy) run() {
	streams, err := tis.cursor.Streams()
	if err != nil {
		log.Println(err)
		return
	}

	for {
		packet, err := tis.cursor.ReadPacket()
		if err != nil {
			break
		}

		packetType := streams[packet.Idx].Type()

		if packetType.IsVideo() {
			_ = tis.WriteSample(webrtc.RTPCodecTypeVideo, media.Sample{
				Data:      packet.Data,
				Timestamp: time.Unix(0, int64(packet.Time.Nanoseconds())),
			})
		}

	}
}

// WriteSample 发送 sample
func (tis *Proxy) WriteSample(kind webrtc.RTPCodecType, sample media.Sample) error {
	if tis.tracksSample == nil || len(tis.tracksSample) == 0 {
		return nil
	}

	for _, tracker := range tis.tracksSample {
		if tracker.Kind() == kind {
			return tracker.WriteSample(sample)
		}
	}

	return nil
}

func (tis *Proxy) NewTrackerSample(kinds []webrtc.RTPCodecType) ([]*webrtc.TrackLocalStaticSample, bool) {

	streams, err := tis.cursor.Streams()
	if err != nil {
		log.Println(err)
		return nil, false
	}

	var trackerList []*webrtc.TrackLocalStaticSample

	for _, stream := range streams {
		switch stream.Type() {
		case av.H264:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.H265:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH265}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.JPEG:
		case av.VP8:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.VP9:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.AV1:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.MJPEG:
		case av.AAC:
		case av.PCM_MULAW:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.PCM_ALAW:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.SPEEX:
		case av.NELLYMOSER:
		case av.PCM:
		case av.OPUS:
			if t, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		}
	}

	for _, stream := range streams {
		switch stream := stream.(type) {
		case *h264parser.CodecData:
			_ = stream
		case h265parser.CodecData:
			// H265
		case av.AudioCodecData:
			//
		case aacparser.CodecData:
			// aac
		case opusparser.CodecData:
			// opus
		}
	}

	return trackerList, true
}

// NewTrackerRTP 创建接收流
func (tis *Proxy) NewTrackerRTP(kinds []webrtc.RTPCodecType) ([]*webrtc.TrackLocalStaticRTP, bool) {
	streams, err := tis.cursor.Streams()
	if err != nil {
		log.Println(err)
		return nil, false
	}

	var trackerList []*webrtc.TrackLocalStaticRTP

	for _, stream := range streams {
		switch stream.Type() {
		case av.H264:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.H265:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH265}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.JPEG:
		case av.VP8:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.VP9:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.AV1:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.MJPEG:
		case av.AAC:
		case av.PCM_MULAW:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.PCM_ALAW:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		case av.SPEEX:
		case av.NELLYMOSER:
		case av.PCM:
		case av.OPUS:
			if t, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "video", "pion"); err == nil {
				trackerList = append(trackerList, t)
			}
		}
	}

	return trackerList, false
}
