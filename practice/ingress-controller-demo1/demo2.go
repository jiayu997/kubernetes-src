package ingress_controller_demo1

import (
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"time"
)

func TestDeploymentInformer() {
	clientSet := getRestClient()

	informerFactory := informers.NewSharedInformerFactory(clientSet, time.Second*30)

	// 仅初始化了一个： deploymentInformer
	deploymentInformer := informerFactory.Apps().V1().Deployments()

	// 为deployment资源类型初始化了一个shareindexinformer, 并且这个informer注册到shareindexinformer Factory中去
	deploymentShareIndexInformer := deploymentInformer.Informer()

	// add EventHandler function
	deploymentShareIndexInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			deployment, _ := newObj.(*appsv1.Deployment)
			fmt.Printf("Update Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
		AddFunc: func(obj interface{}) {
			deployment, _ := obj.(*appsv1.Deployment)
			fmt.Printf("Add Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
		DeleteFunc: func(obj interface{}) {
			deployment, _ := obj.(*appsv1.Deployment)
			fmt.Printf("Delete Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
	})

	//deploymentIndexer := deploymentInformer.Lister()
	var stopChan chan struct{}

	// 启动informer
	informerFactory.Start(stopChan)

	// 等待同步完成
	informerFactory.WaitForCacheSync(stopChan)

	// 查询数据

	// 关闭
	close(stopChan)
}
