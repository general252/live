package webrtc_server

import (
	"fmt"
	"github.com/pion/rtp"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

type Puller struct {
	connPath            string
	websocketConnection *websocket.Conn

	peerConnection *webrtc.PeerConnection

	tracks []*webrtc.TrackLocalStaticRTP
}

func NewPuller(connPath string, conn *websocket.Conn) *Puller {
	return &Puller{
		connPath:            connPath,
		websocketConnection: conn,
	}
}

func (tis *Puller) GetConnPath() string {
	return tis.connPath
}

// OnOffer 拉流请求
func (tis *Puller) OnOffer(request *JsonProtocol, api *webrtc.API, tracks []*webrtc.TrackLocalStaticRTP) error {
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

	tis.tracks = tracks

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

func (tis *Puller) initPeerConnection(peerConnection *webrtc.PeerConnection) error {

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

func (tis *Puller) OnCandidate(request *JsonProtocol) error {
	return nil
}

func (tis *Puller) Write(fromTracker *webrtc.TrackRemote, data []byte) (n int, err error) {
	if tis.tracks == nil || len(tis.tracks) == 0 {
		return len(data), nil
	}

	for _, tracker := range tis.tracks {
		if fromTracker.Kind() == tracker.Kind() {
			return tracker.Write(data)
		}
	}

	return len(data), nil
}

// WriteRTP 发送rtp
func (tis *Puller) WriteRTP(fromTracker *webrtc.TrackRemote, pkt *rtp.Packet) error {
	if tis.tracks == nil || len(tis.tracks) == 0 {
		return nil
	}

	for _, tracker := range tis.tracks {
		if fromTracker.Kind() == tracker.Kind() {
			return tracker.WriteRTP(pkt)
		}
	}

	return nil
}

func (tis *Puller) Close() error {
	if tis.peerConnection != nil {
		_ = tis.peerConnection.Close()
	}

	if tis.websocketConnection != nil {
		_ = tis.websocketConnection.Close()
	}

	return nil
}
