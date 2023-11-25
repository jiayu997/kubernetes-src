package index

import (
	"fmt"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func GetClientSet() *kubernetes.Clientset {
	// staging/src/k8s.io/client-go/tools/clientcmd/client_config.go
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		panic(err)
	}

	// get clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	factory := informers.NewSharedInformerFactory(clientSet, 0)

	// 返回一个deploymentInformer
	deploymentInformer := factory.Apps().V1().Deployments()
	// 实际上已经相当于把deploymentIndexinformer注册到indexinformerFactory中了, 同时添加event handler
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			fmt.Println("update")
		},
		DeleteFunc: func(obj interface{}) {
			fmt.Println("delete")
		},
		AddFunc: func(obj interface{}) {
			fmt.Println("add")
		},
	})
	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)
	<-stopCh
	return clientSet
}
