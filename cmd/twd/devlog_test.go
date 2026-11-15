package main

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestDropLines(t *testing.T) {
	var b bytes.Buffer
	d := dropLines{w: &b, drop: [][]byte{[]byte("STUN server listening on ")}}
	d.Write([]byte("2026/11/14 STUN server listening on 127.0.0.1:1\n"))
	d.Write([]byte("something real\n"))
	if b.String() != "something real\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestCheckDevPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	err = checkDevPort(port, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("busy port: %v", err)
	}
	ln.Close()
	if err := checkDevPort(port, &bytes.Buffer{}); err != nil {
		t.Fatalf("free port: %v", err)
	}
	// Taken on ::1 only: a warning, not an error.
	ln6, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback")
	}
	defer ln6.Close()
	var w bytes.Buffer
	if err := checkDevPort(ln6.Addr().(*net.TCPAddr).Port, &w); err != nil {
		t.Fatalf("::1 only: %v", err)
	}
	if !strings.Contains(w.String(), "[::1]") {
		t.Fatalf("no ::1 warning: %q", w.String())
	}
}
