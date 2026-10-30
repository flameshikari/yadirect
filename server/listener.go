package main

import (
	"net"
	"sync"
)

// Bound accepted sockets, including idle HTTP and hijacked WebSocket connections.
type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newBoundedListener(listener net.Listener, capacity int) net.Listener {
	return &boundedListener{Listener: listener, slots: make(chan struct{}, capacity), done: make(chan struct{})}
}
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConn{Conn: conn, release: func() { <-l.slots }}, nil
}
func (l *boundedListener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

type boundedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
