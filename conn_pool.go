package connpool

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sync/atomic"
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
	maxTTL  time.Duration
	factory ConnFactory

	ch chan *conn

	queueSize int32 // 猜测的最佳的待命连接数
}

func New(maxTTL time.Duration, factory ConnFactory) *AutoConnPool {
	pool := &AutoConnPool{
		maxTTL:    maxTTL,
		queueSize: 0,
		factory:   factory,
		ch:        make(chan *conn, 1024),
	}

	return pool
}

func (p *AutoConnPool) SetMaxIdle(d time.Duration) {
	p.maxTTL = d
}

func (p *AutoConnPool) Size() int {
	return len(p.ch)
}

func (p *AutoConnPool) TargetSize() int {
	return int(p.queueSize)
}

func (p *AutoConnPool) String() string {
	return fmt.Sprintf("[pool %v/%v]", p.Size(), p.TargetSize())
}

func (p *AutoConnPool) safeFactory() (c net.Conn, e error) {
	defer func() {
		if r := recover(); r != nil {
			e = fmt.Errorf("panic: %s", r)
			if c != nil {
				c.Close()
			}
		}
	}()

	c, e = p.factory()
	return
}

func (p *AutoConnPool) makeConn() {
	t := time.Now()
	c, e := p.safeFactory()

	timeSpent := time.Now().Sub(t)
	p.ch <- &conn{
		c:         c,
		Err:       e,
		createdAt: t,
		TimeSpent: timeSpent,
	}

	if e == nil {
		Logger.Printf("%v . %v", p, timeSpent)
	} else {
		Logger.Printf("%v . %v (%v)", p, timeSpent, e)
	}
}

func (p *AutoConnPool) Get() (net.Conn, error) {
	var x *conn
	for {
		select {
		case x = <-p.ch:
			// 连接过老，说明queueSize过大，要丢弃连接并且缩小queueSize
			if time.Now().Sub(x.createdAt) > p.maxTTL {
				atomic.AddInt32(&p.queueSize, -1)
				if x.c != nil {
					x.c.Close()
				}
				continue
			} else {
				go p.makeConn()
			}

		default:
			// 连接池中没有连接，说明queueSize不够大，需要size+1
			atomic.AddInt32(&p.queueSize, 1)
			Logger.Printf("%s +", p)
			go p.makeConn()

			x = <-p.ch
			go p.makeConn()
		}

		Logger.Printf("%s >", p)
		return x.c, x.Err
	}
}
