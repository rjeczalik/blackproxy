package fakerpc

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
)

// ErrAlreadyRunning TODO(rjeczalik): document
var ErrAlreadyRunning = errors.New("fakerpc: server is already running")

// ErrNotRunning TODO(rjeczalik): document
var ErrNotRunning = errors.New("fakerpc: server is not running")

// Transmission TODO(rjeczalik): document
type Transmission struct {
	Src *net.TCPAddr
	Dst *net.TCPAddr
	Raw []byte
}

// Log TODO(rjeczalik): document
type Log struct {
	Network net.IPNet
	Filter  string
	T       []Transmission
}

// NetIP TODO(rjeczalik): document
func (l *Log) NetIP() net.IP {
	return ipnull(l.Network.IP)
}

// NetMask TODO(rjeczalik): document
func (l *Log) NetMask() net.IP {
	return masktoip(l.Network.Mask)
}

// NetFilter TODO(rjeczalik): document
func (l *Log) NetFilter() string {
	if l.Filter != "" {
		return l.Filter
	}
	if len(l.T) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("(ip or ipv6) and ( host %s and port %d )",
		l.T[0].Dst.IP, l.T[0].Dst.Port)
}

// NewLog TODO(rjeczalik): document
func NewLog() *Log {
	return &Log{T: make([]Transmission, 0)}
}

// ReadLog TODO(rjeczalik): document
func ReadLog(file string) (l *Log, err error) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	var buf bytes.Buffer
	l = NewLog()
	r := io.TeeReader(f, &buf)
	dec := gob.NewDecoder(r)
	if err = dec.Decode(l); err == nil {
		return
	}
	err = NgrepUnmarshal(bytes.NewBuffer(buf.Bytes()), l)
	return
}

// WriteLog TODO(rjeczalik): document
func WriteLog(file string, log *Log) error {
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	return enc.Encode(log)
}

// Connection TODO(rjeczalik): document
type Connection struct {
	Req     *http.Request
	ReqBody []byte
	Res     []byte
}

// Connections TODO(rjeczalik): document
type Connections [][]Connection

func equal(lhs, rhs *net.TCPAddr) bool {
	return lhs == rhs || (lhs.IP.Equal(rhs.IP) && lhs.Port == rhs.Port)
}

// NewConnections TODO(rjeczalik): document
func NewConnections(log *Log) (Connections, error) {
	c, index := make(Connections, 0), make(map[string]int)
	for i := 0; i < len(log.T); {
		addr := log.T[i].Src.String()
		n, ok := index[addr]
		if !ok {
			c = append(c, make([]Connection, 0))
			n = len(c) - 1
			index[addr] = n
		}
		header, body := SplitHTTP(log.T[i].Raw)
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewBuffer(header)))
		if err != nil {
			return nil, err
		}
		var conn = Connection{Req: req}
		if len(body) > 0 {
			conn.ReqBody = make([]byte, len(body))
			copy(conn.ReqBody, body)
		}
		if i+1 < len(log.T) && equal(log.T[i].Src, log.T[i+1].Dst) {
			i += 1
			conn.Res = make([]byte, len(log.T[i].Raw))
			copy(conn.Res, log.T[i].Raw)
		}
		i += 1
		c[n] = append(c[n], conn)
	}
	return c, nil
}

// SplitHTTP TODO(rjeczalik): document
func SplitHTTP(p []byte) (header []byte, body []byte) {
	if n := bytes.Index(p, []byte("\r\n\r\n")); n != -1 {
		header = p[:n+4]
		body = p[n+4:]
	}
	return
}
