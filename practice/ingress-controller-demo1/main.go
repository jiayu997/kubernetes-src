package ingress_controller_demo1

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"time"
)

func getClientSet() *kubernetes.Clientset {
	// config
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		clusterConfig, err := rest.InClusterConfig()
		if err != nil {
			panic(err)
		}
		config = clusterConfig
	}

	// clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientSet
}

func ControllerMain() {
	clientSet := getClientSet()

	informerFactory := informers.NewSharedInformerFactory(clientSet, time.Second*30)

	// 仅初始化了一个： serviceInformer, 此时还没有注册到informerFactory中去
	serviceInformer := informerFactory.Core().V1().Services()

	ingressInformer := informerFactory.Networking().V1().Ingresses()

	// 初始化一个自定义controller
	controller := newController(clientSet, serviceInformer, ingressInformer)

	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	controller.Run(stopCh)
}
