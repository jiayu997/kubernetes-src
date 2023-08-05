package _chan

import (
	"fmt"
	"testing"
	"time"
)

func TestChanPtr(t *testing.T) {
	// %v = 0x1400008c2a0
	ch1 := make(chan interface{})

	// %v = nil
	var ch2 chan interface{}
	fmt.Printf("%v\n%v", ch1, ch2)
}

func TestIntoChan(t *testing.T) {
	var ch chan<- interface{}
	for {
		select {
		case ch <- struct{}{}:
			t.Log("into")
		default:
			t.Log("default")
			time.Sleep(2)
		}
	}
}
