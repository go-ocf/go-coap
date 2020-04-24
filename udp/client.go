package udp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-ocf/go-coap/v2/keepalive"
	"github.com/go-ocf/go-coap/v2/message"

	"github.com/go-ocf/go-coap/v2/message/codes"
	coapUDP "github.com/go-ocf/go-coap/v2/message/udp"
	coapNet "github.com/go-ocf/go-coap/v2/net"
)

var defaultDialOptions = dialOptions{
	ctx:            context.Background(),
	maxMessageSize: 64 * 1024,
	heartBeat:      time.Millisecond * 100,
	handler: func(w *ResponseWriter, r *Request) {
		w.SetCode(codes.NotFound)
	},
	errors: func(err error) {
		fmt.Println(err)
	},
	goPool: func(f func() error) error {
		go func() {
			err := f()
			if err != nil {
				fmt.Println(err)
			}
		}()
		return nil
	},
	dialer:    &net.Dialer{Timeout: time.Second * 3},
	keepalive: keepalive.New(),
	net:       "udp",
}

type dialOptions struct {
	ctx            context.Context
	maxMessageSize int
	heartBeat      time.Duration
	handler        HandlerFunc
	errors         ErrorFunc
	goPool         GoPoolFunc
	dialer         *net.Dialer
	keepalive      *keepalive.KeepAlive
	net            string
}

// A DialOption sets options such as credentials, keepalive parameters, etc.
type DialOption interface {
	applyDial(*dialOptions)
}

type ClientConn struct {
	session *Session
}

func Dial(target string, opts ...DialOption) (*ClientConn, error) {
	cfg := defaultDialOptions
	for _, o := range opts {
		o.applyDial(&cfg)
	}

	c, err := cfg.dialer.DialContext(cfg.ctx, cfg.net, target)
	if err != nil {
		return nil, err
	}
	conn, ok := c.(*net.UDPConn)
	if !ok {
		return nil, fmt.Errorf("unsupported connection type: %T", c)
	}

	addr, ok := conn.RemoteAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("cannot get target upd address")
	}

	l := coapNet.NewUDPConn(conn, coapNet.WithHeartBeat(cfg.heartBeat), coapNet.WithErrors(cfg.errors))
	cc := NewClientConn(NewSession(cfg.ctx,
		l,
		addr,
		cfg.handler,
		cfg.maxMessageSize,
		cfg.goPool,
	))

	go func() {
		err := cc.run()
		if err != nil {
			cfg.errors(err)
		}
	}()
	if cfg.keepalive != nil {
		go func() {
			err := cfg.keepalive.Run(cc)
			if err != nil {
				cfg.errors(err)
			}
		}()
	}

	return cc, nil
}

func NewClientConn(session *Session) *ClientConn {
	return &ClientConn{
		session: session,
	}
}

func (cc *ClientConn) Close() error {
	return cc.session.Close()
}

func (cc *ClientConn) Do(req *Request) (*Request, error) {
	token := req.Token()
	if token == nil {
		return nil, fmt.Errorf("invalid token")
	}
	respChan := make(chan *Request, 1)
	err := cc.session.TokenHandler().Insert(token, func(w *ResponseWriter, r *Request) {
		r.Hijack()
		respChan <- r
	})
	if err != nil {
		return nil, fmt.Errorf("cannot add token handler: %w", err)
	}
	defer cc.session.TokenHandler().Pop(token)
	err = cc.session.WriteRequest(req)
	if err != nil {
		return nil, fmt.Errorf("cannot write request: %w", err)
	}

	select {
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	case <-cc.session.Context().Done():
		return nil, fmt.Errorf("connection was closed: %w", req.ctx.Err())
	case resp := <-respChan:
		return resp, nil
	}
}

func (cc *ClientConn) doWithMID(req *Request) (*Request, error) {
	respChan := make(chan *Request, 1)
	err := cc.session.midHandlerContainer.Insert(req.MessageID(), func(w *ResponseWriter, r *Request) {
		r.Hijack()
		respChan <- r
	})
	if err != nil {
		return nil, fmt.Errorf("cannot insert mid handler: %w", err)
	}
	defer cc.session.midHandlerContainer.Pop(req.MessageID())
	err = cc.session.WriteRequest(req)
	if err != nil {
		return nil, fmt.Errorf("cannot write request: %w", err)
	}

	select {
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	case <-cc.session.Context().Done():
		return nil, fmt.Errorf("connection was closed: %w", req.ctx.Err())
	case resp := <-respChan:
		return resp, nil
	}
}

func NewGetRequest(ctx context.Context, path string, queries ...string) (*Request, error) {
	token, err := message.GetToken()
	if err != nil {
		return nil, fmt.Errorf("cannot get token: %w", err)
	}
	req := AcquireRequest(ctx)
	req.SetCode(codes.GET)
	req.SetToken(token)
	req.SetPath(path)
	for _, q := range queries {
		req.AddQuery(q)
	}
	return req, nil
}

func (cc *ClientConn) Get(ctx context.Context, path string, queries ...string) (*Request, error) {
	req, err := NewGetRequest(ctx, path, queries...)
	if err != nil {
		return nil, fmt.Errorf("cannot create get request: %w", err)
	}
	defer ReleaseRequest(req)
	return cc.Do(req)
}

func (cc *ClientConn) Context() context.Context {
	return cc.session.Context()
}

func (cc *ClientConn) Ping(ctx context.Context) error {
	req := AcquireRequest(ctx)
	defer ReleaseRequest(req)
	req.SetType(coapUDP.Confirmable)
	req.SetCode(codes.Empty)
	req.SetMessageID(cc.session.getMID())
	resp, err := cc.doWithMID(req)
	if err != nil {
		return err
	}
	defer ReleaseRequest(resp)
	if resp.Type() == coapUDP.Reset || resp.Type() == coapUDP.Acknowledgement {
		return nil
	}
	return fmt.Errorf("unexpected response(%v)", resp)
}

func (cc *ClientConn) run() error {
	m := make([]byte, ^uint16(0))
	for {
		n, _, err := cc.session.connection.ReadWithContext(cc.session.ctx, m)
		if err != nil {
			cc.session.Close()
			return err
		}
		m = m[:n]
		err = cc.session.processBuffer(m)
		if err != nil {
			cc.session.Close()
			return err
		}
	}
}