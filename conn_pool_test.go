package connpool

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
	"net"
)

func init() {
	Logger.SetOutput(os.Stdout)
}

func TestPoolNew(t *testing.T) {
	pool := New(time.Second*20, func() (net.Conn, error) {
		return net.Dial("tcp4", "baidu.com:80")
	})
	assert.Equal(t, 0, pool.Size(), "初始化的时候应该不建立连接")

	for i := 0; i < 10; i++ {
		go pool.Get()
	}
	time.Sleep(time.Second)

	now := time.Now()
	pool.Get()
	assert.True(t, time.Now().Sub(now) < time.Millisecond, "快速从缓存中取连接")
	assert.True(t, pool.Size() >= 8)
}
