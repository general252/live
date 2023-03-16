package server_interface

import (
	"github.com/deepch/vdk/av/pubsub"
)

type Channel struct {
	Que *pubsub.Queue
}

type ServerInterface interface {
	GetChannel(connPath string) (*Channel, bool)
	CreateChannel(connPath string) (*Channel, bool)
	RemoteChannel(connPath string)
}
