package fakerpc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

var errNoResponse = errors.New("fakerpc: no response recorded for the request")

var noopReply = func(*net.TCPAddr, *net.TCPAddr, int64, error) {}

func write500(rw net.Conn, err error) {
	s := err.Error()
	io.WriteString(rw, fmt.Sprintf("HTTP/1.1 500 Internal Server Error\r\n"+
		"Content-Length: %d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		len(s), s))
}

// A Server represents a HTTP server, which serves connections by replying with
// recorded responses.
type Server struct {
	// Reply function is called after each transmission is successfully completed.
	Reply func(src, dst *net.TCPAddr, bodyLen int64, err error)
	m     sync.Mutex
	wg    sync.WaitGroup
	wgr   sync.WaitGroup
	conn  Connections
	l     net.Listener
	src   *net.TCPAddr
	addr  string
	isrun uint32
	count int
}

// NewServer gives new Server for the given address and log.
func NewServer(addr string, log *Log) (srv *Server, err error) {
	srv = &Server{Reply: noopReply, addr: addr}
	srv.wgr.Add(1)
	srv.conn, err = NewConnections(log)
	return
}

// ServeConn copies a response from c for every of the rw coonection's request.
func (srv *Server) ServeConn(rw net.Conn, c []Connection) {
	var (
		n   int64
		err error
		req *http.Request
		r   = bufio.NewReader(rw)
		rem = tcpaddrnil(rw.RemoteAddr())
	)
	for i := 0; ; i++ {
		if req, err = http.ReadRequest(r); err != nil {
			break
		}
		n, err = io.Copy(ioutil.Discard, req.Body)
		req.Body.Close()
		if i >= len(c) {
			write500(rw, errNoResponse)
			srv.Reply(rem, srv.src, n, errNoResponse)
			continue
		}
		srv.Reply(rem, srv.src, n, err)
		if err != nil {
			write500(rw, err)
			continue
		}
		if c[i].Res != nil {
			_, err = io.Copy(rw, bytes.NewBuffer(c[i].Res))
			srv.Reply(srv.src, rem, int64(len(c[i].Res)), err)
		}
	}
	if err != nil && err != io.EOF {
		srv.Reply(rem, srv.src, 0, err)
	}
	rw.Close()
	srv.wg.Done()
}

// ListenAndServe starts the server which handles only specific number of
// connections, determined by the Log argument given during creation of
// the server; after those connections were served, Server stops itself.
func (srv *Server) ListenAndServe() (err error) {
	if atomic.CompareAndSwapUint32(&srv.isrun, 0, 1) {
		srv.m.Lock()
		defer func() {
			// Ignore "use of closed network connection" comming from closed
			// net.Listener when p was explicitely stopped.
			if !atomic.CompareAndSwapUint32(&srv.isrun, 1, 0) {
				err = nil
			}
		}()
		if srv.l, err = net.Listen("tcp", srv.addr); err != nil {
			srv.m.Unlock()
			return
		}
		if srv.src, err = tcpaddr(srv.l.Addr()); err != nil {
			srv.m.Unlock()
			return
		}
		srv.wgr.Done()
		srv.m.Unlock()
		var (
			c    []Connection
			conn net.Conn
		)
		for {
			if conn, err = srv.l.Accept(); err != nil {
				return
			}
			c = srv.conn[srv.count]
			srv.count += 1
			srv.wg.Add(1)
			go srv.ServeConn(conn, c)
			if srv.count == len(srv.conn) {
				srv.Stop()
				break
			}
		}
		srv.wg.Wait()
		return
	}
	return ErrAlreadyRunning
}

// Addr returns the Server's network address. It blocks if the srv is not running.
func (srv *Server) Addr() (addr net.Addr) {
	srv.wgr.Wait()
	srv.m.Lock()
	addr = srv.l.Addr()
	srv.m.Unlock()
	return
}

// Stop stops the Server from accepting new connections. It waits for on-going
// connections to finish.
func (srv *Server) Stop() (err error) {
	err = ErrNotRunning
	if atomic.CompareAndSwapUint32(&srv.isrun, 1, 0) {
		srv.wgr.Wait()
		srv.m.Lock()
		err = srv.l.Close()
		srv.wgr.Add(1)
		srv.m.Unlock()
	}
	return
}
