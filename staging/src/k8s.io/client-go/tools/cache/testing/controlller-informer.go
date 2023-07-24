package framework

import (
	"fmt"
	"k8s.io/klog/v2"
	"testing"
)

func TestControllerInformer(t *testing.T) {
	// 实例化 apiserver 客户端
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	// 实例化 pod 类型的 list watcher 对象
	podListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "pods", v1.NamespaceDefault, fields.Everything())

	// 实例化支持 indexer 自定义索引方法的 informer
	indexer, informer := cache.NewIndexerInformer(podListWatcher, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			fmt.Println(1)
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			fmt.Println(1)
		},
		DeleteFunc: func(obj interface{}) {
			fmt.Println(1)
		},
	}, cache.Indexers{})

	// 启动 informer
	go informer.Run(stopCh)

	// 等待同步数据到本地缓存
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		return
	}
	...
}