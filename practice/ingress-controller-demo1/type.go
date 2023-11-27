package ingress_controller_demo1

import (
	"k8s.io/client-go/kubernetes"
	coreV1 "k8s.io/client-go/listers/core/v1"
	networkV1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/util/workqueue"
)

type Controller struct {
	Client        *kubernetes.Clientset
	ServiceLister coreV1.ServiceLister
	IngressLister networkV1.IngressLister
	Queue         workqueue.RateLimitingInterface
}

const (
	CEVENTADD    event  = "ADD"
	CEVENTDELETE event  = "DELETE"
	CEVENTUPDATE event  = "UPDATE"
	SERVICE      string = "Service"
	INGRESS      string = "Ingress"
)

type event string

type Cevent struct {
	objectKey string
	Type      event
	Kind      string
}
