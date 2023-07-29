package demo1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

type testLw struct {
	ListFunc  func(options v1.ListOptions) (runtime.Object, error)
	WatchFunc func(options v1.ListOptions) (watch.Interface, error)
}
