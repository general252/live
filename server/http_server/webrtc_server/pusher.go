package webrtc_server

import (
	"fmt"
	"github.com/pion/rtp"
	"log"
	"time"

	"github.com/general252/live/util"
	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

type Pusher struct {
	connPath            string
	websocketConnection *websocket.Conn

	receiver *util.Map[uint64, *Puller]

	peerConnection *webrtc.PeerConnection
}

func NewPusher(connPath string, conn *websocket.Conn) *Pusher {
	return &Pusher{
		connPath:            connPath,
		websocketConnection: conn,
		receiver:            util.NewMap[uint64, *Puller](),
	}
}

func (tis *Pusher) GetConnPath() string {
	return tis.connPath
}

func (tis *Pusher) Close() error {
	var receivers []*Puller
	tis.receiver.Range(func(key uint64, value *Puller) bool {
		receivers = append(receivers, value)
		return true
	})

	for _, receiver := range receivers {
		_ = receiver.Close()
	}

	if tis.peerConnection != nil {
		_ = tis.peerConnection.Close()
	}

	if tis.websocketConnection != nil {
		_ = tis.websocketConnection.Close()
	}

	return nil
}

func (tis *Pusher) OnCandidate(request *JsonRequest) error {
	return nil
}

// OnOffer 推流请求
func (tis *Pusher) OnOffer(request *JsonRequest, api *webrtc.API) error {
	if request.Data.Offer == nil {
		return fmt.Errorf("offer sdp is nil")
	}
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

	//
	tis.peerConnection = peerConnection

	// Set the remoteWebrtc SessionDescription
	if err = peerConnection.SetRemoteDescription(*request.Data.Offer); err != nil {
		return err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Create answer
	answer, err := peerConnection.CreateAnswer(&webrtc.AnswerOptions{
		OfferAnswerOptions: webrtc.OfferAnswerOptions{
			VoiceActivityDetection: false,
		},
	})
	if err != nil {
		return err
	}

	// Set local SDP
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		return err
	}

	log.Println("wait PeerConnection complete")

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	// 接收数据
	tis.onTracks(peerConnection)

	// 回复
	localSDP := peerConnection.LocalDescription()

	reply := &JsonResponse{
		Method: Answer,
		Code:   0,
		Msg:    "success",
		Data: JsonResponsePayload{
			Answer: localSDP,
		},
	}
	if err = wsConnection.WriteJSON(reply); err != nil {
		return err
	}

	return nil
}

func (tis *Pusher) initPeerConnection(peerConnection *webrtc.PeerConnection) error {
	var err error

	// 接收视频
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		log.Println(err)
		return err
	}

	// 接收音频
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		log.Println(err)
		return err
	}

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
	})

	return nil
}

func (tis *Pusher) onTracks(peerConnection *webrtc.PeerConnection) {

	var oneTrack = func(remoteTrack *webrtc.TrackRemote) {
		buffer := make([]byte, 1500)
		for {
			n, _, err := remoteTrack.Read(buffer)
			if err != nil {
				log.Println(err)
				break
			}

			var receivers []*Puller
			tis.receiver.Range(func(key uint64, value *Puller) bool {
				receivers = append(receivers, value)
				return true
			})

			for _, receiver := range receivers {
				_, _ = receiver.Write(remoteTrack, buffer[:n])
			}

			if false {
				var packet rtp.Packet
				if err := packet.Unmarshal(buffer[:n]); err != nil {
					log.Println(err)
				}
			}
		}

	}

	for _, receiver := range peerConnection.GetReceivers() {
		for _, remoteTrack := range receiver.Tracks() {
			go oneTrack(remoteTrack)
		}
	}

}

func (tis *Pusher) AddReceiver(receiver *Puller) {
	tis.receiver.Store(receiver.GetID(), receiver)
}

func (tis *Pusher) DelReceiver(receiver *Puller) {
	tis.receiver.Delete(receiver.GetID())
}

// NewTrackerRTP 创建接收流
func (tis *Pusher) NewTrackerRTP(kinds []webrtc.RTPCodecType) ([]*webrtc.TrackLocalStaticRTP, bool) {
	if tis.peerConnection == nil {
		return nil, false
	}

	var rtpTrackerList []*webrtc.TrackLocalStaticRTP

	for _, receiver := range tis.peerConnection.GetReceivers() {
		for _, tracker := range receiver.Tracks() {
			for _, kind := range kinds {
				if tracker.Kind() == kind {
					t, err := webrtc.NewTrackLocalStaticRTP(tracker.Codec().RTPCodecCapability, tracker.Kind().String(), "pion")
					if err != nil {
						log.Printf("NewTrackLocalStaticRTP fail. %v", err)
						break
					}

					rtpTrackerList = append(rtpTrackerList, t)
				}
			}
		}
	}

	return rtpTrackerList, true
}
