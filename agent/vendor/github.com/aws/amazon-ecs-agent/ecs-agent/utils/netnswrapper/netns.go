package netnswrapper

import (
	"github.com/vishvananda/netns"
)

type NetNsWrapper interface {
	GetFromPath(path string) (netns.NsHandle, error)
	Set(handle netns.NsHandle) error
	CloseHandle(handle *netns.NsHandle) error
	Get() (netns.NsHandle, error)
}

type netNs struct{}

func New() NetNsWrapper {
	return &netNs{}
}

func (ns *netNs) Get() (netns.NsHandle, error) {
	return netns.Get()
}

func (ns *netNs) GetFromPath(path string) (netns.NsHandle, error) {
	return netns.GetFromPath(path)
}

func (ns *netNs) Set(handle netns.NsHandle) error {
	return netns.Set(handle)
}

func (ns *netNs) CloseHandle(handle *netns.NsHandle) error {
	return handle.Close()
}
