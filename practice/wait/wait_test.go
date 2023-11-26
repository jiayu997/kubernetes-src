package wait

import (
	"k8s.io/apimachinery/pkg/util/wait"
	"testing"
	"time"
)

// 就是启动一个协程，每隔一定的时间，就去运行声明的匿名函数，直到接收到结束信号 就关闭这个协程
func TestWait(t *testing.T) {
	stopCh := make(chan struct{})

	//初始化一个计数器
	i := 0
	go wait.Until(func() {
		t.Logf("----%d----\n", i)
		i++
	}, time.Second, stopCh)

	time.Sleep(time.Second * 10)

	stopCh <- struct{}{}

	t.Logf("---上面的go routines 结束----\n")

	// 主程序，再休息3秒钟，再结束
	time.Sleep(time.Second * 3)

	t.Logf("---主程序结束----\n")
}

func TestRangeWait(t *testing.T) {
	stopCh := make(chan struct{})

	for i := 0; i < 5; i++ {
		i := i
		go wait.Until(func() {
			t.Logf("---%d---time: %v", i, time.Now())
		}, time.Second, stopCh)
	}
	time.Sleep(time.Second * 10)

	stopCh <- struct{}{}

	t.Logf("---上面的go routines 结束----\n")

	// 主程序，再休息3秒钟，再结束
	time.Sleep(time.Second * 3)

	t.Logf("---主程序结束----\n")
}
