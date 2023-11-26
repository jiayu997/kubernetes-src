package ingress_controller_demo1

import (
	coreV1 "k8s.io/client-go/informers/core/v1"
	networkV1 "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func (c *Controller) addService(obj interface{}) {

}

func (c *Controller) updateService(obj interface{}, obj2 interface{}) {

}

func (c *Controller) deleteService(obj interface{}) {

}

func (c *Controller) addIngress(obj interface{}) {

}

func (c *Controller) updateIngress(obj interface{}, obj2 interface{}) {

}

func (c *Controller) deleteIngress(obj interface{}) {

}

func newController(clientSet *kubernetes.Clientset, serviceInformer coreV1.ServiceInformer, ingressInformer networkV1.IngressInformer) *Controller {
	c := Controller{
		client:        clientSet,
		serviceLister: serviceInformer.Lister(),
		ingressLister: ingressInformer.Lister(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Controller"),
	}

	// 添加service event handler
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addService,
		UpdateFunc: c.updateService,
		DeleteFunc: c.deleteService,
	})

	// 添加ingress event handler
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addIngress,
		UpdateFunc: c.updateIngress,
		DeleteFunc: c.deleteIngress,
	})

	return &c
}

func (c *Controller) Run(stopCh chan struct{}) {
}
