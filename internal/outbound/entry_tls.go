package outbound

import (
	"context"
	"net"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

// A stable outbound lets pooled HTTP transports use the current setting on
// each dial. Create the second adapter only after the policy changes and the
// node is used, so changing a setting does not rebuild the entire node pool
// or close existing sessions.
type entryTLSOutbound struct {
	adapter.Outbound
	initialInsecure bool
	allowInsecure   func() bool
	buildAlternate  func() (adapter.Outbound, error)

	mu        sync.Mutex
	alternate adapter.Outbound
	closed    bool
}

func (o *entryTLSOutbound) current() (adapter.Outbound, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, net.ErrClosed
	}
	if o.initialInsecure == o.allowInsecure() {
		return o.Outbound, nil
	}
	if o.alternate == nil {
		next, err := o.buildAlternate()
		if err != nil {
			return nil, err
		}
		o.alternate = next
	}
	return o.alternate, nil
}

func (o *entryTLSOutbound) DialContext(ctx context.Context, network string, target M.Socksaddr) (net.Conn, error) {
	current, err := o.current()
	if err != nil {
		return nil, err
	}
	return current.DialContext(ctx, network, target)
}

func (o *entryTLSOutbound) ListenPacket(ctx context.Context, target M.Socksaddr) (net.PacketConn, error) {
	current, err := o.current()
	if err != nil {
		return nil, err
	}
	return current.ListenPacket(ctx, target)
}

func (o *entryTLSOutbound) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.closed = true
		closeOutbound(o.Outbound)
		closeOutbound(o.alternate)
	}
	return nil
}
