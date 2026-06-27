//go:build linux

package capture

import (
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
)

// RunLive 在指定网卡上用 AF_PACKET 被动抓包(无需 libpcap)。阻塞运行。
func (e *Engine) RunLive(iface string) error {
	tp, err := afpacket.NewTPacket(
		afpacket.OptInterface(iface),
		afpacket.OptPollTimeout(time.Second),
	)
	if err != nil {
		return err
	}
	defer tp.Close()
	src := gopacket.NewPacketSource(tp, layers.LayerTypeEthernet)
	src.NoCopy = true
	e.process(src)
	return nil
}
