package shareinformer

import (
	"fmt"
	flag "github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"time"
)

func Demo1() {
	var err error
	var config *rest.Config
	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "")
	}

	// init reset.config
	if config, err = rest.InClusterConfig(); err != nil {
		if config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig); err != nil {
			panic(err.Error())
		}
	}

	// init client set
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// init former factory
	informerFactory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	// 实例化了一个deploymentInformer
	// &deploymentInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
	deploymentInformer := informerFactory.Apps().V1().Deployments()

	// 返回一个：cache.SharedIndexInformer: sharedIndexInformer
	informer := deploymentInformer.Informer()

	// get lister, 返回一个本地deployment Indexer
	_ = deploymentInformer.Lister()

	// add event handler, 这里会后台生产事件监听器，一直去处理事件(在没启动的时候，还没有事件发过来)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			fmt.Println("add")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			fmt.Println("update")
		},
		DeleteFunc: func(obj interface{}) {
			fmt.Println("delete")
		},
	})

	stopper := make(chan struct{})
	defer close(stopper)

	// start informer, list & watch
	informerFactory.Start(stopper)

	// wait informer sync indexer
	informerFactory.WaitForCacheSync(stopper)

	<-stopper
}
