package connpool

import (
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

type ConnFactory func() (net.Conn, error)

type AutoConnPool struct {
	sync.Mutex

	maxTTL  time.Duration
	factory ConnFactory

	conns chan *conn

	queueSize uint // 猜测的最佳的待命连接数
}

func New(maxTTL time.Duration, factory ConnFactory) *AutoConnPool {
	pool := &AutoConnPool{
		maxTTL:    maxTTL,
		queueSize: 1,
		factory:   factory,
	}

	return pool
}

func (p *AutoConnPool) SetMaxIdle(d time.Duration) {
	p.maxTTL = d
}

func (p *AutoConnPool) Size() int {
	return len(p.conns)
}

func (p *AutoConnPool) TargetSize() int {
	return int(p.queueSize)
}

func (p *AutoConnPool) String() string {
	return fmt.Sprintf("[pool %v/%v]", p.Size(), p.TargetSize())
}

func (p *AutoConnPool) makeConn() {
	t := time.Now()
	c, e := p.factory()

	timeSpent := time.Now().Sub(t)
	p.conns <- &conn{
		c:         c,
		Err:       e,
		createdAt: t,
		TimeSpent: timeSpent,
	}

	Logger.Printf("%v . %v (%v)", p, timeSpent, e)
}

func (p *AutoConnPool) Get() (net.Conn, error) {
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
		if time.Now().Sub(pc.createdAt) > p.maxTTL {
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
