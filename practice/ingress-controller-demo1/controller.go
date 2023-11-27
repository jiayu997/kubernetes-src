package ingress_controller_demo1

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	coreV1 "k8s.io/client-go/informers/core/v1"
	networkV1 "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"reflect"
)

// 当创建一个service时，检查是否
func (c *Controller) addService(obj interface{}) {
	c.enqueue(obj, CEVENTADD, SERVICE)
}

func (c *Controller) updateService(oldObj interface{}, newObj interface{}) {
	// todo 比较annotation
	// 这里只是比较了对象是否相同，如果相同，直接返回
	if reflect.DeepEqual(oldObj, newObj) {
		return
	}

	c.enqueue(newObj, CEVENTUPDATE, SERVICE)
}

func (c *Controller) deleteService(obj interface{}) {
	c.enqueue(obj, CEVENTDELETE, SERVICE)
}

// 添加ingress不管
func (c *Controller) addIngress(obj interface{}) {
	return

}

// 更新ingress不管
func (c *Controller) updateIngress(obj interface{}, obj2 interface{}) {
	return
}

// 删除ingress不管
func (c *Controller) deleteIngress(obj interface{}) {
	return
}

func (c *Controller) enqueue(object interface{}, e event, kind string) {
	// 计算object key
	key, err := cache.MetaNamespaceKeyFunc(object)
	if err != nil {
		runtime.HandleError(err)
	}

	cEvent := Cevent{
		objectKey: key,
		Type:      e,
		Kind:      kind,
	}

	// key放到队列中去
	c.Queue.Add(&cEvent)
}

func newController(clientSet *kubernetes.Clientset, serviceInformer coreV1.ServiceInformer, ingressInformer networkV1.IngressInformer) *Controller {
	c := Controller{
		Client:        clientSet,
		ServiceLister: serviceInformer.Lister(),
		IngressLister: ingressInformer.Lister(),
		Queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Controller"),
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
	c.worker()
}

func (c *Controller) worker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	// 获取队列中的key
	item, shutdown := c.Queue.Get()
	if shutdown {
		return false
	}

	// 当我们处理完成后，要将这个itemKey标记为完成
	defer c.Queue.Done(item)

	// 将队列取出来的数据丢到相应的函数里头去
	cEvent, ok := item.(*Cevent)
	if !ok {
		return false
	}
	//fmt.Println(cEvent.objectKey, cEvent.Type, cEvent.Kind)

	switch cEvent.Kind {
	case SERVICE:
		if !c.processServiceItem(cEvent) {
			return false
		}
	case INGRESS:
		if !c.processIngressItem(cEvent) {
			return false
		}
	default:
		return false
	}
	return true
}

func (c *Controller) processServiceItem(cEvent *Cevent) bool {
	serviceNamespace, name, err := cache.SplitMetaNamespaceKey(cEvent.objectKey)
	if err != nil {
		fmt.Printf("%v\n", err)
		return false
	}

	// 获取service
	service, err := c.ServiceLister.Services(serviceNamespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return true
		}
		fmt.Printf("%v\n", err)
		return false
	}

	switch cEvent.Type {
	case CEVENTADD:
		if !c.serviceAddAndUpdateHandlerFunc(service) {
			return false
		}
	case CEVENTUPDATE:
		if !c.serviceAddAndUpdateHandlerFunc(service) {
			return false
		}
	case CEVENTDELETE:
		if !c.serviceDeleteHandlerFunc(service) {
			return false
		}
	default:
		return false
	}

	return true
}

func (c *Controller) serviceAddAndUpdateHandlerFunc(service *v1.Service) bool {
	// 获取service的annotation
	annotationMap := service.GetAnnotations()
	ingressSwitch, ok := annotationMap["ingress"]

	// 当service没有这个annotation或者ingress != true时，不用处理
	if !ok || ingressSwitch == "false" {
		return true
	}

	// 获取ingress
	ingress, err := c.IngressLister.Ingresses(service.Namespace).Get(service.Name)
	if err != nil && !errors.IsNotFound(err) {
		return false
	}

	// 创建ingress(ingress不存在)
	if ok && errors.IsNotFound(err) {
		ingress = constructIngress(service)
		_, err := c.Client.NetworkingV1().Ingresses(service.Namespace).Create(context.TODO(), ingress, metaV1.CreateOptions{})
		if err != nil {
			fmt.Printf("Ingress Namespace: %s Name: %s Create Failed\nError: %v", ingress.Namespace, ingress.Name, err)
			return false
		} else {
			fmt.Printf("Ingress Namespace: %s Name: %s Create Success\n", ingress.Namespace, ingress.Name)
			return true
		}
	}

	// ingress已经存在,此时进行状态维护
	if ok {
		ingress = constructIngress(service)
		_, err := c.Client.NetworkingV1().Ingresses(service.Namespace).Update(context.TODO(), ingress, metaV1.UpdateOptions{})
		if err != nil {
			fmt.Printf("Ingress Namespace: %s Name: %s Update Failed\nError: %v", ingress.Namespace, ingress.Name, err)
			return false
		} else {
			fmt.Printf("Ingress Namespace: %s Name: %s Update Success\n", ingress.Namespace, ingress.Name)
			return true
		}
	}
	return true
}

func (c *Controller) serviceDeleteHandlerFunc(service *v1.Service) bool {
	// service创建ingress时，有OwnerReferences，删除service会自动删除ingress
	return true
}

func (c *Controller) processIngressItem(cEvent *Cevent) bool {
	return true
}
