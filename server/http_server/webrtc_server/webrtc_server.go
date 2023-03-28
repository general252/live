package webrtc_server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/general252/live/server/server_interface"
	"github.com/general252/live/util"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
)

type WebrtcServer struct {
	parent server_interface.ServerInterface

	api     *webrtc.API
	pushers *util.Map[string, *Pusher]
}

func NewWebrtcServer(parent server_interface.ServerInterface) *WebrtcServer {
	engine := &WebrtcServer{
		parent:  parent,
		pushers: util.NewMap[string, *Pusher](),
	}

	muxUdpPort := 7000
	engine.api = webrtc.NewAPI(engine.getMuxOptions(muxUdpPort)...)
	return engine
}

func (tis *WebrtcServer) OnPusher(c *gin.Context) {
	var websocketUpGrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// 获取参数
	connPath := "/" + c.Param("ConnPath")
	log.Println(connPath)
	defer func() {
		log.Printf("%v 推流请求结束", connPath)
	}()

	// 检查是否存在
	if tis.pushers.IsExist(connPath) {
		var reply = JsonResponse{
			Method: Answer,
			Code:   1,
			Msg:    fmt.Sprintf("have exist %v", connPath),
			Data: JsonResponsePayload{
				Answer: nil,
			},
		}
		c.JSON(http.StatusOK, &reply)
		return
	}

	// 升级为websocket connection
	conn, err := websocketUpGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// 创建pusher对象
	objectPusher := NewPusher(connPath, conn)

	defer func() {
		tis.pushers.Delete(connPath)
		_ = objectPusher.Close()
		_ = conn.Close()
	}()

	tis.pushers.Store(connPath, objectPusher)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)

		var request JsonRequest
		if err = json.Unmarshal(message, &request); err != nil {
			log.Println(err)
			break
		}

		// 收到请求
		switch request.Method {
		case Offer:
			err = objectPusher.OnOffer(&request, tis.api)
		case Candidate:
			err = objectPusher.OnCandidate(&request)
		default:
			err = fmt.Errorf("unknown method %v", request.Method)
		}

		if err != nil {
			log.Println(err)
			break
		}
	}

}

func (tis *WebrtcServer) OnPlayer(c *gin.Context) {
	var websocketUpGrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// 获取参数
	connPath := "/" + c.Param("ConnPath")
	log.Println(connPath)
	defer func() {
		log.Printf("%v 拉流请求结束", connPath)
	}()

	// 检查是否存在
	objectPusher, ok := tis.pushers.Load(connPath)
	if !ok {
		tis.onProxy(c)
		return
	}

	tracks, ok := objectPusher.NewTrackerRTP([]webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio})
	if !ok {
		var reply = JsonResponse{
			Method: Answer,
			Code:   1,
			Msg:    fmt.Sprintf("no found tracks"),
			Data:   JsonResponsePayload{},
		}
		c.JSON(http.StatusOK, reply)
		return
	}

	// 升级为websocket connection
	conn, err := websocketUpGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	objectPuller := NewPuller(connPath, conn)

	defer func() {
		objectPusher.DelReceiver(objectPuller)
		_ = objectPuller.Close()
		_ = conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)

		var request JsonRequest
		if err = json.Unmarshal(message, &request); err != nil {
			log.Println(err)
			break
		}

		// 收到请求
		switch request.Method {
		case Offer:
			if err = objectPuller.OnOffer(&request, tis.api, tracks); err == nil {
				objectPusher.AddReceiver(objectPuller)
			}
		case Candidate:
			err = objectPuller.OnCandidate(&request)
		default:
			err = fmt.Errorf("unknown method %v", request.Method)
		}

		if err != nil {
			log.Println(err)
			break
		}
	}
}

func (tis *WebrtcServer) onProxy(c *gin.Context) {
	var websocketUpGrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// 获取参数
	connPath := "/" + c.Param("ConnPath")
	log.Println(connPath)

	// 检查是否存在
	sourceChannel, ok := tis.parent.GetChannel(connPath)
	if !ok {
		var reply = JsonResponse{
			Method: Answer,
			Code:   1,
			Msg:    fmt.Sprintf("not found %v", connPath),
			Data:   JsonResponsePayload{},
		}
		c.JSON(http.StatusOK, reply)
		return
	}

	// 升级为websocket connection
	conn, err := websocketUpGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	objectProxy := NewProxy(connPath, conn, sourceChannel)

	defer func() {
		_ = objectProxy.Close()
		_ = conn.Close()
	}()

	tracks, ok := objectProxy.NewTrackerRTP([]webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio})
	if !ok {
		var reply = JsonResponse{
			Method: Answer,
			Code:   1,
			Msg:    fmt.Sprintf("no found tracks"),
			Data:   JsonResponsePayload{},
		}
		c.JSON(http.StatusOK, reply)
		return
	}

	defer func() {
		_ = conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)

		var request JsonRequest
		if err = json.Unmarshal(message, &request); err != nil {
			log.Println(err)
			break
		}

		// 收到请求
		switch request.Method {
		case Offer:
			err = objectProxy.OnOffer(&request, tis.api, tracks)
		case Candidate:
			err = objectProxy.OnCandidate(&request)
		default:
			err = fmt.Errorf("unknown method %v", request.Method)
		}

		if err != nil {
			log.Println(err)
			break
		}
	}
}

func (tis *WebrtcServer) getMuxOptions(muxUdpPort int) []func(*webrtc.API) {
	// Listen on UDP Port 2000, will be used for all WebRTC traffic
	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: muxUdpPort,
	})
	if err != nil {
		panic(err)
	}

	_ = udpListener.SetWriteBuffer(512 * 1024)
	_ = udpListener.SetReadBuffer(512 * 1024)

	log.Printf("Listening for WebRTC traffic at %s\n", udpListener.LocalAddr())

	var options []func(*webrtc.API)

	{
		// Create a SettingEngine, this allows non-standard WebRTC behavior
		settingEngine := webrtc.SettingEngine{}

		// Configure our SettingEngine to use our UDPMux. By default a PeerConnection has
		// no global state. The API+SettingEngine allows the user to share state between them.
		// In this case we are sharing our listening port across many.
		settingEngine.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udpListener))

		options = append(options, webrtc.WithSettingEngine(settingEngine))
	}

	// Create a MediaEngine object to configure the supported codec
	m := &webrtc.MediaEngine{}
	{
		var codecParams = []webrtc.RTPCodecParameters{
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: nil},
				PayloadType:        96,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000, SDPFmtpLine: "profile-id=0", RTCPFeedback: nil},
				PayloadType:        98,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9, ClockRate: 90000, SDPFmtpLine: "profile-id=1", RTCPFeedback: nil},
				PayloadType:        100,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f", RTCPFeedback: nil},
				PayloadType:        125,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f", RTCPFeedback: nil},
				PayloadType:        108,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=640032", RTCPFeedback: nil},
				PayloadType:        123,
			},
			{
				RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1, ClockRate: 90000, RTCPFeedback: nil},
				PayloadType:        35,
			},
		}

		if false {
			err = m.RegisterCodec(
				webrtc.RTPCodecParameters{
					RTPCodecCapability: webrtc.RTPCodecCapability{
						MimeType:     webrtc.MimeTypeH264,
						ClockRate:    90000,
						Channels:     0,
						SDPFmtpLine:  "",
						RTCPFeedback: nil,
					},
					PayloadType: 96,
				},
				webrtc.RTPCodecTypeVideo,
			)
			if err != nil {
				panic(err)
			}
		} else {
			for _, param := range codecParams {
				err = m.RegisterCodec(param, webrtc.RTPCodecTypeVideo)
				if err != nil {
					panic(err)
				}
			}
		}

		err = m.RegisterCodec(
			webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeOpus,
					ClockRate:    48000,
					Channels:     0,
					SDPFmtpLine:  "",
					RTCPFeedback: nil,
				},
				PayloadType: 111,
			},
			webrtc.RTPCodecTypeAudio,
		)
		if err != nil {
			panic(err)
		}
	}

	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}

	options = append(options, webrtc.WithMediaEngine(m))
	options = append(options, webrtc.WithInterceptorRegistry(i))

	return options
}
