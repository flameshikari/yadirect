package main

import (
	"net"
	"testing"
	"time"
)

func TestSocketLimitAndShutdown(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := newBoundedListener(raw, 1)
	defer listener.Close()
	first, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	second, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	done := make(chan net.Conn, 1)
	go func() { conn, _ := listener.Accept(); done <- conn }()
	select {
	case conn := <-done:
		if conn != nil {
			conn.Close()
		}
		t.Fatal("accepted beyond socket capacity")
	case <-time.After(20 * time.Millisecond):
	}
	accepted.Close()
	accepted.Close()
	select {
	case conn := <-done:
		if conn == nil {
			t.Fatal("connection capacity was not released")
		}
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("blocked after releasing a socket")
	}
	go func() { conn, _ := listener.Accept(); done <- conn }()
	listener.Close()
	select {
	case conn := <-done:
		if conn != nil {
			conn.Close()
			t.Fatal("accept succeeded after closing listener")
		}
	case <-time.After(time.Second):
		t.Fatal("listener shutdown blocked")
	}
}
