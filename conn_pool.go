package connpool

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"
)

var Logger *log.Logger

func init() {
	Logger = log.New(ioutil.Discard, "connpool", log.LstdFlags)
}

type conn struct {
	c         net.Conn
	Err       error
	createdAt time.Time
	TimeSpent time.Duration
}

type ConnPool struct {
	sync.Mutex

	addr      string
	ssl       bool
	conns     chan *conn
	maxIdle   time.Duration
	queueSize uint // 猜测的最佳的待命连接数
}

func New(addr string, ssl bool, maxIdle time.Duration) *ConnPool {
	pool := &ConnPool{
		addr:      addr,
		ssl:       ssl,
		maxIdle:   maxIdle,
		queueSize: 1,
	}

	return pool
}

func (p *ConnPool) SetMaxIdle(d time.Duration) {
	p.maxIdle = d
}

func (p *ConnPool) Size() int {
	return len(p.conns)
}

func (p *ConnPool) TargetSize() int {
	return int(p.queueSize)
}

func (p *ConnPool) String() string {
	return fmt.Sprintf("[pool %s %v/%v]", p.addr, p.Size(), p.TargetSize())
}

func (p *ConnPool) makeConn() {
	var c net.Conn
	var e error

	t := time.Now()
	e = Do(func(attempt int) (retry bool, err error) {
		c, err = net.DialTimeout("tcp4", p.addr, time.Second*time.Duration(attempt))
		return attempt < 3, err
	})
	if e == nil && c != nil && p.ssl {
		var host string
		if host, _, e = net.SplitHostPort(p.addr); e == nil {
			c, e = sslHandshake(c, host)
		}
	}

	timeSpent := time.Now().Sub(t)
	p.conns <- &conn{
		c:         c,
		Err:       e,
		createdAt: t,
		TimeSpent: timeSpent,
	}

	Logger.Printf("%v . %v (%v)", p, timeSpent, e)
}

func (p *ConnPool) Get() (net.Conn, error) {
	p.Lock()
	defer p.Unlock()

	var i uint

	// 初次Get时,自动初始化连接池
	if p.conns == nil {
		p.conns = make(chan *conn, 1024)
		for i = 0; i < p.queueSize; i++ {
			go p.makeConn()
		}
	}

	var pc *conn
	for {
		// 连接不够用时,增大备用连接数
		if len(p.conns) == 0 {
			p.queueSize += 1
			Logger.Printf("%s +", p)
			go p.makeConn()
		}

		pc = <-p.conns

		// 如果获得的conn生成时间过早,则减少备用连接数
		if time.Now().Sub(pc.createdAt) > p.maxIdle {
			p.queueSize -= 1
			if pc.c != nil {
				pc.c.Close()
			}
			continue // 重新获取一个连接
		} else {
			go p.makeConn()
		}

		Logger.Printf("%s >", p)
		return pc.c, pc.Err
	}
}

func sslHandshake(conn net.Conn, host string) (net.Conn, error) {
	secureConn := tls.Client(conn, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	})
	err := secureConn.Handshake()
	if err != nil {
		conn.Close()
		return nil, err
	} else {
		return secureConn, nil
	}
}
